package raft

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"
	"runtime"

	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
)

// TODO: this is just copied over from cmd/utils/cmd.go. dedupe

// Fatalf formats a message to standard error and exits the program.
// The message is also printed to standard output if standard error
// is redirected to a different file.
func fatalf(format string, args ...interface{}) {
	w := io.MultiWriter(os.Stdout, os.Stderr)
	if runtime.GOOS == "windows" {
		// The SameFile check below doesn't work on Windows.
		// stdout is unlikely to get redirected though, so just print there.
		w = os.Stdout
	} else {
		outf, _ := os.Stdout.Stat()
		errf, _ := os.Stderr.Stat()
		if outf != nil && errf != nil && os.SameFile(outf, errf) {
			w = os.Stderr
		}
	}
	fmt.Fprintf(w, "Fatal: "+format+"\n", args...)
	os.Exit(1)
}

// maps an enode id (512 bit public key of the node) to a uint16 raft id.
// TODO/note: (2020 Sept 20) the underlying ectd raft id is a uint64, but for quorum backwards compatibility / upgrade, we are keepting this
//            a uint16, in the near future, we maybe want to provide an upgrade path for raft64
// the enode ids for the network are obtained from the static-nodes.json file, or passed in when running raft.addPeer(enodeURL).
// nodeIdToRaftId takes the Keccak hash of the enode id, which helps to avoid collisions and
// ensure uniqueness, and converts the hash into an uint64 which is the raftId type used by the etcd
// raft protocol.
func nodeIdToRaftId(enodeId string) (uint16, error) {
	log.Info("Converting node id to raft id", "enode id", enodeId)
	// Get the keccak hash of of the enodeId to help ensure uniqueness and protect against collisions.
	hashedEnodeId := crypto.Keccak256([]byte(enodeId))
	log.Info("raft hashedEnodeId ", "hashedEnodeId", hashedEnodeId)
	raftId := binary.BigEndian.Uint16(hashedEnodeId)
	log.Info("raft raftId ", "raftId", raftId)
	return raftId, nil
}

// used to convert a uint64 to a string, needed for displaying the raftId properly in Javascript (raft/api.go), e.g.
// raft.addPeer(enodeUrl) as JS Number and uint64 are not compatible.
func RaftIdToString(raftId uint64) string {
	return fmt.Sprintf("%d", raftId)
}
