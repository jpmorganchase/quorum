package raft

import (
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/coreos/etcd/raft/raftpb"
	mapset "github.com/deckarep/golang-set"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/node"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
)

const TEST_URL = "enode://3d9ca5956b38557aba991e31cf510d4df641dce9cc26bfeb7de082f0c07abb6ede3a58410c8f249dabeecee4ad3979929ac4c7c496ad20b8cfdd061b7401b4f5@127.0.0.1:21003?discport=0&raftport=50404"
const ENODE_URL_NODE1 = "enode://ac6b1096ca56b9f6d004b779ae3728bf83f8e22453404cc3cef16a3d9b96608bc67c4b30db88e0a5a6c6390213f7acbe1153ff6d23ce57380104288ae19373ef@quorum-node1:30303?discport=0&raftport=50401"
const ENODE_URL_NODE2 = "enode://0ba6b9f606a43a95edc6247cdb1c1e105145817be7bcafd6b2c0ba15d58145f0dc1a194f70ba73cd6f4cdd6864edc7687f311254c7555cc32e4d45aeb1b80416@quorum-node2:30303?discport=0&raftport=50401"
const ENODE_URL_NODE3 = "enode://579f786d4e2830bbcc02815a27e8a9bacccc9605df4dc6f20bcc1a6eb391e7225fff7cb83e5b4ecd1f3a94d8b733803f2f66b7e871961e7b029e22c155c3a778@quorum-node3:30303?discport=0&raftport=50401"
const ENODE_URL_NODE4 = "enode://3d9ca5956b38557aba991e31cf510d4df641dce9cc26bfeb7de082f0c07abb6ede3a58410c8f249dabeecee4ad3979929ac4c7c496ad20b8cfdd061b7401b4f5@quorum-node4:30303?discport=0&raftport=50401"
const ENODE_URL_NODE5 = "enode://3701f007bfa4cb26512d7df18e6bbd202e8484a6e11d387af6e482b525fa25542d46ff9c99db87bd419b980c24a086117a397f6d8f88e74351b41693880ea0cb@quorum-node5:30303?discport=0&raftport=50401"
const ENODE_URL_NODE6 = "enode://eacaa74c4b0e7a9e12d2fe5fee6595eda841d6d992c35dbbcc50fcee4aa86dfbbdeff7dc7e72c2305d5a62257f82737a8cffc80474c15c611c037f52db1a3a7b@quorum-node6:30303?discport=0&raftport=50401"
const ENODE_URL_NODE7 = "enode://239c1f044a2b03b6c4713109af036b775c5418fe4ca63b04b1ce00124af00ddab7cc088fc46020cdc783b6207efe624551be4c06a994993d8d70f684688fb7cf@quorum-node7:30303?discport=0&raftport=50401"

const ENODE_ID_NODE1 = "ac6b1096ca56b9f6d004b779ae3728bf83f8e22453404cc3cef16a3d9b96608bc67c4b30db88e0a5a6c6390213f7acbe1153ff6d23ce57380104288ae19373ef"
const ENODE_ID_NODE2 = "0ba6b9f606a43a95edc6247cdb1c1e105145817be7bcafd6b2c0ba15d58145f0dc1a194f70ba73cd6f4cdd6864edc7687f311254c7555cc32e4d45aeb1b80416"
const ENODE_ID_NODE3 = "579f786d4e2830bbcc02815a27e8a9bacccc9605df4dc6f20bcc1a6eb391e7225fff7cb83e5b4ecd1f3a94d8b733803f2f66b7e871961e7b029e22c155c3a778"
const ENODE_ID_NODE4 = "3d9ca5956b38557aba991e31cf510d4df641dce9cc26bfeb7de082f0c07abb6ede3a58410c8f249dabeecee4ad3979929ac4c7c496ad20b8cfdd061b7401b4f5"
const ENODE_ID_NODE5 = "3701f007bfa4cb26512d7df18e6bbd202e8484a6e11d387af6e482b525fa25542d46ff9c99db87bd419b980c24a086117a397f6d8f88e74351b41693880ea0cb"
const ENODE_ID_NODE6 = "eacaa74c4b0e7a9e12d2fe5fee6595eda841d6d992c35dbbcc50fcee4aa86dfbbdeff7dc7e72c2305d5a62257f82737a8cffc80474c15c611c037f52db1a3a7b"
const ENODE_ID_NODE7 = "239c1f044a2b03b6c4713109af036b775c5418fe4ca63b04b1ce00124af00ddab7cc088fc46020cdc783b6207efe624551be4c06a994993d8d70f684688fb7cf"

func TestSignHeader(t *testing.T) {
	//create only what we need to test the seal
	testRaftId := getRaftId(t, ENODE_ID_NODE5) // 5
	config := &node.Config{Name: "unit-test", DataDir: ""}

	nodeKey := config.NodeKey()

	raftProtocolManager := &ProtocolManager{raftId: testRaftId}
	raftService := &RaftService{nodeKey: nodeKey, raftProtocolManager: raftProtocolManager}
	minter := minter{eth: raftService}

	//create some fake header to sign
	fakeParentHash := common.HexToHash("0xc2c1dc1be8054808c69e06137429899d")

	header := &types.Header{
		ParentHash: fakeParentHash,
		Number:     big.NewInt(1),
		Difficulty: big.NewInt(1),
		GasLimit:   uint64(0),
		GasUsed:    uint64(0),
		Coinbase:   minter.coinbase,
		Time:       uint64(time.Now().UnixNano()),
	}

	headerHash := header.Hash()
	extraDataBytes := minter.buildExtraSeal(headerHash)
	var seal *extraSeal
	err := rlp.DecodeBytes(extraDataBytes[:], &seal)
	if err != nil {
		t.Fatalf("Unable to decode seal: %s", err.Error())
	}

	// Check raftId
	sealRaftId, err := hexutil.DecodeUint64("0x" + string(seal.RaftId)) //add the 0x prefix
	if err != nil {
		t.Errorf("Unable to get RaftId: %s", err.Error())
	}
	if sealRaftId != uint64(testRaftId) {
		t.Errorf("RaftID does not match. Expected: %d, Actual: %d", testRaftId, sealRaftId)
	}

	//Identify who signed it
	sig := seal.Signature
	pubKey, err := crypto.SigToPub(headerHash.Bytes(), sig)
	if err != nil {
		t.Fatalf("Unable to get public key from signature: %s", err.Error())
	}

	//Compare derived public key to original public key
	if pubKey.X.Cmp(nodeKey.X) != 0 {
		t.Errorf("Signature incorrect!")
	}

}

func TestAddLearner_whenTypical(t *testing.T) {
	raftIdNode1 := getRaftId(t, ENODE_ID_NODE1)
	raftService := newTestRaftService(t, []uint64{raftIdNode1}, []uint64{})
	raftService.raftProtocolManager.setRaftId(raftIdNode1)

	propPeer := func() {
		raftid, err := raftService.raftProtocolManager.ProposeNewPeer(TEST_URL, true)
		raftService.raftProtocolManager.setRaftId(raftid)
		if err != nil {
			t.Errorf("propose new peer failed %v\n", err)
		}
		if raftid != raftService.raftProtocolManager.raftId {
			t.Errorf("1. wrong raft id. expected %d got %d\n", raftService.raftProtocolManager.raftId, raftid)
		}
	}
	go propPeer()
	select {
	case confChange := <-raftService.raftProtocolManager.confChangeProposalC:
		if confChange.Type != raftpb.ConfChangeAddLearnerNode {
			t.Errorf("expected ConfChangeAddLearnerNode but got %s", confChange.Type.String())
		}
		if confChange.NodeID != raftService.raftProtocolManager.raftId {
			t.Errorf("2. wrong raft id. expected %d got %d\n", raftService.raftProtocolManager.raftId, confChange.NodeID)
		}
	case <-time.After(time.Millisecond * 200):
		t.Errorf("add learner conf change not received")
	}
}

func TestPromoteLearnerToPeer_whenTypical(t *testing.T) {
	// TODO: get enode id for node
	//learnerRaftId := uint16(3)
	raftIdLearner3 := getRaftId(t, ENODE_ID_NODE3)
	raftIdNode2 := getRaftId(t, ENODE_ID_NODE2)
	raftService := newTestRaftService(t, []uint64{raftIdNode2}, []uint64{raftIdLearner3})
	raftService.raftProtocolManager.setRaftId(raftIdNode2)

	promoteToPeer := func() {
		ok, err := raftService.raftProtocolManager.PromoteToPeer(raftIdLearner3)
		if err != nil || !ok {
			t.Errorf("promote learner to peer failed %v\n", err)
		}
	}
	go promoteToPeer()
	select {
	case confChange := <-raftService.raftProtocolManager.confChangeProposalC:
		if confChange.Type != raftpb.ConfChangeAddNode {
			t.Errorf("expected ConfChangeAddNode but got %s", confChange.Type.String())
		}
		if confChange.NodeID != raftIdLearner3 {
			t.Errorf("2. wrong raft id. expected %d got %d\n", raftIdLearner3, confChange.NodeID)
		}
	case <-time.After(time.Millisecond * 200):
		t.Errorf("add learner conf change not received")
	}
}

func TestAddLearnerOrPeer_fromLearner(t *testing.T) {
	raftIdNode2 := getRaftId(t, ENODE_ID_NODE2)
	raftIdLearner3 := getRaftId(t, ENODE_ID_NODE3)
	raftService := newTestRaftService(t, []uint64{raftIdNode2}, []uint64{raftIdLearner3})
	raftService.raftProtocolManager.setRaftId(raftIdLearner3)
	_, err := raftService.raftProtocolManager.ProposeNewPeer(TEST_URL, true)

	if err == nil {
		t.Errorf("learner should not be allowed to add learner or peer")
	}

	if err != nil && !strings.Contains(err.Error(), "learner node can't add peer or learner") {
		t.Errorf("expect error message: propose new peer failed, got: %v\n", err)
	}

	_, err = raftService.raftProtocolManager.ProposeNewPeer(TEST_URL, false)

	if err == nil {
		t.Errorf("learner should not be allowed to add learner or peer")
	}

	if err != nil && !strings.Contains(err.Error(), "learner node can't add peer or learner") {
		t.Errorf("expect error message: propose new peer failed, got: %v\n", err)
	}

}

func TestPromoteLearnerToPeer_fromLearner(t *testing.T) {
	raftIdNode1 := getRaftId(t, ENODE_ID_NODE1)
	raftIdNode2 := getRaftId(t, ENODE_ID_NODE2)
	raftIdLearner3 := getRaftId(t, ENODE_ID_NODE3)
	raftService := newTestRaftService(t, []uint64{raftIdNode1}, []uint64{raftIdNode2, raftIdLearner3})
	raftService.raftProtocolManager.setRaftId(raftIdNode2)

	_, err := raftService.raftProtocolManager.PromoteToPeer(raftIdLearner3)

	if err == nil {
		t.Errorf("learner should not be allowed to promote to peer")
	}

	if err != nil && !strings.Contains(err.Error(), "learner node can't promote to peer") {
		t.Errorf("expect error message: propose new peer failed, got: %v\n", err)
	}

}

func enodeId(id string, ip string, raftPort int) string {
	return fmt.Sprintf("enode://%s@%s?discport=0&raftport=%d", id, ip, raftPort)
}

func getRaftId(t *testing.T, enodeId string) uint64 {
	raftId, err := nodeIdToRaftId(enodeId)
	if err != nil {
		t.Fatalf("Unable convert enode id: %s to raft Id. error: %s", enodeId, err.Error())
	}
	return uint64(raftId)
}

func peerList(url string) (error, []*enode.Node) {
	var nodes []*enode.Node
	node, err := enode.ParseV4(url)
	if err != nil {
		return fmt.Errorf("Node URL %s: %v\n", url, err), nil
	}
	nodes = append(nodes, node)
	return nil, nodes
}

func newTestRaftService(t *testing.T, nodes []uint64, learners []uint64) *RaftService {
	//create only what we need to test add learner node
	config := &node.Config{Name: "unit-test", DataDir: ""}
	nodeKey := config.NodeKey()
	enodeIdStr := fmt.Sprintf("%x", crypto.FromECDSAPub(&nodeKey.PublicKey)[1:])
	url := enodeId(enodeIdStr, "127.0.0.1:21001", 50401)
	err, peers := peerList(url)
	if err != nil {
		t.Errorf("getting peers failed %v", err)
	}
	raftProtocolManager := &ProtocolManager{
		bootstrapNodes:      peers,
		confChangeProposalC: make(chan raftpb.ConfChange),
		removedPeers:        mapset.NewSet(),
		confState:           raftpb.ConfState{Nodes: nodes, Learners: learners},
	}
	raftService := &RaftService{nodeKey: nodeKey, raftProtocolManager: raftProtocolManager}
	return raftService
}
