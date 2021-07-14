package raft

const (
	//protocolName           = "raft"
	//protocolVersion uint64 = 0x01

	//raftMsg = 0x00

	//verifierRole = etcdRaft.NOT_LEADER

	// Raft's ticker interval
	tickerMS = 100

	// We use a bounded channel of constant size buffering incoming messages
	//msgChanSize = 1000

	// Snapshot after this many raft messages
	//
	// TODO: measure and get this as low as possible without affecting performance
	//
	snapshotPeriod = 250

	//peerUrlKeyPrefix = "peerUrl-"

	chainExtensionMessage = "Successfully extended chain"
)

var (
	appliedDbKey = []byte("applied")
)
