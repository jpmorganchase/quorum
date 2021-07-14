package raft

import (
	"crypto/ecdsa"
	"encoding/binary"
	"fmt"
	"io/ioutil"
	"net"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/params"
	etcdRaft "go.etcd.io/etcd/raft/v3"
	"go.etcd.io/etcd/server/v3/wal"
	"go.etcd.io/etcd/server/v3/wal/walpb"
	"go.uber.org/zap"
)

// pm.advanceAppliedIndex() and state updates are in different
// transaction boundaries hence there's a probablity that they are
// out of sync due to premature shutdown
func TestProtocolManager_whenAppliedIndexOutOfSync(t *testing.T) {
	logger := log.New()
	logger.SetHandler(log.StreamHandler(os.Stdout, log.TerminalFormat(false)))
	tmpWorkingDir, err := ioutil.TempDir("", "")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = os.RemoveAll(tmpWorkingDir)
	}()
	count := 3
	ports := make([]uint16, count)
	nodeKeys := make([]*ecdsa.PrivateKey, count)
	peers := make([]*enode.Node, count)
	for i := 0; i < count; i++ {
		ports[i] = nextPort(t)
		nodeKeys[i] = mustNewNodeKey(t)
		peers[i] = enode.NewV4Hostname(&(nodeKeys[i].PublicKey), net.IPv4(127, 0, 0, 1).String(), 0, 0, int(ports[i]))
	}
	raftNodes := make([]*RaftService, count)
	for i := 0; i < count; i++ {
		if s, err := startRaftNode(uint16(i+1), ports[i], tmpWorkingDir, nodeKeys[i], peers); err != nil {
			t.Fatal(err)
		} else {
			raftNodes[i] = s
		}
	}
	waitFunc := func() {
		for {
			time.Sleep(10 * time.Millisecond)
			for i := 0; i < count; i++ {
				if raftNodes[i].raftProtocolManager.role == int(etcdRaft.StateLeader) {
					return
				}
			}
		}
	}
	waitFunc()
	logger.Debug("stop the cluster")
	for i := 0; i < count; i++ {
		if err := raftNodes[i].Stop(); err != nil {
			t.Fatal(err)
		}
		// somehow the wal dir is still being locked that causes failures in subsequent start
		// we need to check here to make sure everything is fully stopped
		for isWalDirStillLocked(fmt.Sprintf("%s/node%d/raft-wal", tmpWorkingDir, i+1)) {
			logger.Debug("sleep...", "i", i)
			time.Sleep(10 * time.Millisecond)
		}
		logger.Debug("node stopped", "id", i)
	}
	logger.Debug("update applied index")
	// update the index to mimic the issue (set applied index behind for node 0)
	if err := writeAppliedIndex(tmpWorkingDir, 0, 1); err != nil {
		t.Fatal(err)
	}
	//time.Sleep(3 * time.Second)
	logger.Debug("restart the cluster")
	for i := 0; i < count; i++ {
		if s, err := startRaftNode(uint16(i+1), ports[i], tmpWorkingDir, nodeKeys[i], peers); err != nil {
			t.Fatal(err)
		} else {
			raftNodes[i] = s
		}
	}
	waitFunc()
}

func isWalDirStillLocked(walDir string) bool {
	var snap walpb.Snapshot
	// TODO: @achraf
	lg, _ := zap.NewProduction()
	w, err := wal.Open(lg, walDir, snap)
	if err != nil {
		return true
	}
	defer func() {
		_ = w.Close()
	}()
	return false
}

func writeAppliedIndex(workingDir string, node int, index uint64) error {
	db, err := openQuorumRaftDb(fmt.Sprintf("%s/node%d/quorum-raft-state", workingDir, node+1))
	if err != nil {
		return err
	}
	defer func() {
		_ = db.Close()
	}()
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint64(buf, index)
	return db.Put(appliedDbKey, buf, noFsync)
}

func mustNewNodeKey(t *testing.T) *ecdsa.PrivateKey {
	k, err := crypto.GenerateKey()
	if err != nil {
		t.Fatal(err)
	}
	return k
}

func nextPort(t *testing.T) uint16 {
	listener, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	return uint16(listener.Addr().(*net.TCPAddr).Port)
}

func prepareServiceContext(key *ecdsa.PrivateKey) (stack *node.Node, cfg *node.Config, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("%s", r)
			stack = nil
			cfg = nil
		}
	}()
	cfg = &node.Config{
		P2P: p2p.Config{
			PrivateKey: key,
		},
	}
	stack, _ = node.New(cfg)
	return
}

func startRaftNode(id, port uint16, tmpWorkingDir string, key *ecdsa.PrivateKey, nodes []*enode.Node) (*RaftService, error) {
	raftlogdir := fmt.Sprintf("%s/node%d", tmpWorkingDir, id)

	stack, _, err := prepareServiceContext(key)
	if err != nil {
		return nil, err
	}

	e, err := eth.New(stack, &eth.Config{
		Genesis: &core.Genesis{Config: params.QuorumTestChainConfig},
	})
	if err != nil {
		return nil, err
	}

	s, err := New(stack, params.QuorumTestChainConfig, id, port, false, 100*time.Millisecond, e, nodes, raftlogdir, false)
	if err != nil {
		return nil, err
	}

	if err := stack.Server().Start(); err != nil {
		return nil, fmt.Errorf("could not start: %v", err)
	}
	if err := s.Start(); err != nil {
		return nil, err
	}

	return s, nil
}
