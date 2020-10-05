package raft

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/coreos/etcd/etcdserver/stats"
	"github.com/coreos/etcd/pkg/fileutil"
	raftTypes "github.com/coreos/etcd/pkg/types"
	etcdRaft "github.com/coreos/etcd/raft"
	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/rafthttp"
	"github.com/coreos/etcd/snap"
	"github.com/coreos/etcd/wal"
	mapset "github.com/deckarep/golang-set"
	"github.com/syndtr/goleveldb/leveldb"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/rlp"
)

type ProtocolManager struct {
	mu       sync.RWMutex // For protecting concurrent JS access to "local peer" and "remote peer" state
	quitSync chan struct{}
	stopped  bool

	// Static configuration
	joinExisting   bool // TODO: this is kept around for interacting with < 2.6 nodes, should deprecate.
	bootstrapNodes []*enode.Node
	raftId         uint64
	raftPort       uint16

	// Local peer state (protected by mu vs concurrent access via JS)
	address       *Address
	role          int    // Role: minter or verifier
	appliedIndex  uint64 // The index of the last-applied raft entry
	snapshotIndex uint64 // The index of the latest snapshot.

	// Remote peer state (protected by mu vs concurrent access via JS)
	leader       uint64
	peers        map[uint64]*Peer
	removedPeers mapset.Set // *Permanently removed* peers

	// P2P transport
	p2pServer *p2p.Server // Initialized in start()
	useDns    bool

	// Blockchain services
	blockchain *core.BlockChain
	downloader *downloader.Downloader
	minter     *minter

	// Blockchain events
	eventMux      *event.TypeMux
	minedBlockSub *event.TypeMuxSubscription

	// Raft proposal events
	blockProposalC      chan *types.Block      // for mined blocks to raft
	confChangeProposalC chan raftpb.ConfChange // for config changes from js console to raft

	// Raft transport
	unsafeRawNode etcdRaft.Node
	transport     *rafthttp.Transport
	httpstopc     chan struct{}
	httpdonec     chan struct{}

	// Raft snapshotting
	snapshotter *snap.Snapshotter
	snapdir     string
	confState   raftpb.ConfState

	// Raft write-ahead log
	waldir string
	wal    *wal.WAL

	// Storage
	quorumRaftDb *leveldb.DB             // Persistent storage for last-applied raft index
	raftStorage  *etcdRaft.MemoryStorage // Volatile raft storage
}

var errNoLeaderElected = errors.New("no leader is currently elected")

//
// Public interface
//
// note at this point we do not know the raft id, we will obtain the raft id from the node id when the service is started.
func NewProtocolManager(raftId uint64, raftPort uint16, blockchain *core.BlockChain, mux *event.TypeMux, bootstrapNodes []*enode.Node, joinExisting bool, datadir string, minter *minter, downloader *downloader.Downloader, useDns bool) (*ProtocolManager, error) {
	waldir := fmt.Sprintf("%s/raft-wal", datadir)
	snapdir := fmt.Sprintf("%s/raft-snap", datadir)
	quorumRaftDbLoc := fmt.Sprintf("%s/quorum-raft-state", datadir)

	manager := &ProtocolManager{
		bootstrapNodes:      bootstrapNodes,
		peers:               make(map[uint64]*Peer),
		leader:              etcdRaft.None,
		removedPeers:        mapset.NewSet(),
		joinExisting:        joinExisting,
		blockchain:          blockchain,
		eventMux:            mux,
		blockProposalC:      make(chan *types.Block, 10),
		confChangeProposalC: make(chan raftpb.ConfChange),
		httpstopc:           make(chan struct{}),
		httpdonec:           make(chan struct{}),
		waldir:              waldir,
		snapdir:             snapdir,
		snapshotter:         snap.New(snapdir),
		raftId:              raftId,
		raftPort:            raftPort,
		quitSync:            make(chan struct{}),
		raftStorage:         etcdRaft.NewMemoryStorage(),
		minter:              minter,
		downloader:          downloader,
		useDns:              useDns,
	}

	if db, err := openQuorumRaftDb(quorumRaftDbLoc); err != nil {
		return nil, err
	} else {
		manager.quorumRaftDb = db
	}

	return manager, nil
}

// Used for testing only
func (pm *ProtocolManager) setRaftId(raftId uint64) {
	pm.raftId = raftId
}

func (pm *ProtocolManager) Start(p2pServer *p2p.Server) {
	log.Info("starting raft protocol handler")

	pm.p2pServer = p2pServer
	pm.minedBlockSub = pm.eventMux.Subscribe(core.NewMinedBlockEvent{})
	// Raft Id must be unique, we therefore derive it from the enode id, which we get from the p2p.Server. This
	// is not available when the the raft Protocol Manager is first initialized.
	enode := enode.MustParse(pm.p2pServer.NodeInfo().Enode)

	// TODO: This is kept around for interop with < 2.6 quorum raft node, think about deprecating.
	if pm.joinExisting { // raftId is already set in this case.
		log.Info("raftId has been passed in with --raftjoinexisting", "raftId", pm.raftId)
	} else {
		raftId, err := nodeIdToRaftId(enode.EnodeID())
		if err != nil {
			log.Error("Error converting pm.p2pServer.NodeInfo().Enode to uint64 raftId", "pm.p2pServer.NodeInfo().Enode", pm.p2pServer.NodeInfo().Enode, "err", err)
			panic(fmt.Sprintf("Error converting enodeId (pm.p2pServer.NodeInfo().Enode) to uint64 raftId. endoeId is [%v] error is [%v]", pm.p2pServer.NodeInfo().Enode, err))
		}
		// Override what was previously set..
		pm.raftId = uint64(raftId)

		log.Info("raftId obtained from enode id", "raftId", raftId)
	}
	pm.startRaft()
	// update raft peers info to p2p server
	pm.p2pServer.SetCheckPeerInRaft(pm.peerExist)
	go pm.minedBroadcastLoop()
}

func (pm *ProtocolManager) Stop() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	defer log.Info("raft protocol handler stopped")

	if pm.stopped {
		return
	}

	log.Info("stopping raft protocol handler...")

	for raftId, peer := range pm.peers {
		pm.disconnectFromPeer(raftId, peer)
	}

	pm.minedBlockSub.Unsubscribe()

	if pm.transport != nil {
		pm.transport.Stop()
	}

	close(pm.httpstopc)
	<-pm.httpdonec
	close(pm.quitSync)

	if pm.unsafeRawNode != nil {
		pm.unsafeRawNode.Stop()
	}

	pm.quorumRaftDb.Close()

	pm.p2pServer = nil

	pm.minter.stop()

	pm.stopped = true
}

func (pm *ProtocolManager) NodeInfo() *RaftNodeInfo {
	pm.mu.RLock() // as we read role and peers
	defer pm.mu.RUnlock()

	roleDescription := ""
	if pm.role == minterRole {
		roleDescription = "minter"
	} else if pm.isVerifierNode() {
		roleDescription = "verifier"
	} else if pm.isLearnerNode() {
		roleDescription = "learner"
	}

	peerAddresses := make([]*Address, len(pm.peers))
	peerIdx := 0
	for _, peer := range pm.peers {
		peerAddresses[peerIdx] = peer.address
		//log.Info("peer address: ", "peer.address", peer.address)
		peerIdx += 1
	}

	removedPeerIfaces := pm.removedPeers
	removedPeerIds := make([]uint64, removedPeerIfaces.Cardinality())
	i := 0
	for removedIface := range removedPeerIfaces.Iterator().C {
		removedPeerIds[i] = removedIface.(uint64)
		i++
	}

	//
	// NOTE: before exposing any new fields here, make sure that the underlying
	// ProtocolManager members are protected from concurrent access by pm.mu!
	//
	return &RaftNodeInfo{
		ClusterSize:    len(pm.peers) + 1,
		Role:           roleDescription,
		Address:        pm.address,
		PeerAddresses:  peerAddresses,
		RemovedPeerIds: removedPeerIds,
		AppliedIndex:   pm.appliedIndex,
		SnapshotIndex:  pm.snapshotIndex,
	}
}

// There seems to be a very rare race in raft where during `etcdRaft.StartNode`
// it will call back our `Process` method before it's finished returning the
// `raft.Node`, `pm.unsafeRawNode`, to us. This re-entrance through a separate
// thread will cause a nil pointer dereference. To work around this, this
// getter method should be used instead of reading `pm.unsafeRawNode` directly.
func (pm *ProtocolManager) rawNode() etcdRaft.Node {
	for pm.unsafeRawNode == nil {
		time.Sleep(100 * time.Millisecond)
	}

	return pm.unsafeRawNode
}

// Has the raftId been removed? If an id is removed, it can be added back again when making a config change ConfChangeAddNode.
func (pm *ProtocolManager) isRaftIdRemoved(raftId uint64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.removedPeers.Contains(raftId)
}

func (pm *ProtocolManager) isRaftIdUsed(raftId uint64) bool {
	if pm.raftId == raftId || pm.isRaftIdRemoved(raftId) {
		return true
	}

	pm.mu.RLock()
	defer pm.mu.RUnlock()

	return pm.peers[raftId] != nil
}

func (pm *ProtocolManager) isRaftIdInCluster(raftId uint64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, peer := range pm.peers {
		if peer.address.RaftId == uint16(raftId) {
			return true
		}
	}
	return false
}

func (pm *ProtocolManager) peerExist(node *enode.Node) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	for _, p := range pm.peers {
		if node.ID() == p.p2pNode.ID() {
			return true
		}
	}
	return false
}

func (pm *ProtocolManager) ProposeNewPeer(enodeId string, isLearner bool) (uint64, error) {
	if pm.isLearnerNode() {
		return 0, errors.New("learner node can't add peer or learner")
	}
	node, err := enode.ParseV4(enodeId)
	if err != nil {
		return 0, err
	}

	if !pm.useDns {
		// hostname is not allowed if DNS is not enabled
		if node.Host() != "" {
			return 0, fmt.Errorf("raft must enable dns to use hostname")
		}
		if len(node.IP()) != 4 {
			return 0, fmt.Errorf("expected IPv4 address (with length 4), but got IP of length %v", len(node.IP()))
		}
	}

	if !node.HasRaftPort() {
		return 0, fmt.Errorf("enodeId is missing raftport querystring parameter: %v", enodeId)
	}
	raftId, err := nodeIdToRaftId(node.EnodeID())
	if err != nil {
		log.Error("error getting uint64 raft id for proposed new peer ", "node.EnodeID()", node.EnodeID())
		return 0, err
	}
	if pm.isRaftIdInCluster(uint64(raftId)) { // TODO: Libby change this, if it is in the cluster return the Id and a bool.
		log.Error("raftId is already in cluster", "raft id", raftId)
		return 0, fmt.Errorf("raftId is already in cluster raft id [%d]", raftId)
	}

	// we are proposing a node, that may have been previously remove, if it was previously removed, allow it to be added again.
	if pm.removedPeers.Contains(raftId) {
		log.Info("adding peer that is in the removed list, delete it from the removed list so it can be "+
			"re-added.", "raftId", raftId, "pm.removedPeers", pm.removedPeers)
		pm.removedPeers.Remove(raftId)
	}
	// TODO: can we have two address representations? Old (uint16) and new (uint64)? always use new if set in genesis block or blocknumber?
	address := newAddress(raftId, node.RaftPort(), node, pm.useDns)

	confChangeType := raftpb.ConfChangeAddNode

	if isLearner {
		confChangeType = raftpb.ConfChangeAddLearnerNode
	}

	pm.confChangeProposalC <- raftpb.ConfChange{
		Type:    confChangeType,
		NodeID:  uint64(raftId),
		Context: address.toBytes(),
	}

	return uint64(raftId), nil
}

func (pm *ProtocolManager) ProposePeerRemoval(raftId uint64) error {
	if pm.isLearnerNode() && raftId != pm.raftId {
		return errors.New("learner node can't remove other peer")
	}
	pm.confChangeProposalC <- raftpb.ConfChange{
		Type:   raftpb.ConfChangeRemoveNode,
		NodeID: raftId,
	}
	return nil
}

func (pm *ProtocolManager) PromoteToPeer(raftId uint64) (bool, error) {
	if pm.isLearnerNode() {
		return false, errors.New("learner node can't promote to peer")
	}

	if !pm.isLearner(raftId) {
		return false, fmt.Errorf("%d is not a learner. only learner can be promoted to peer", raftId)
	}

	pm.confChangeProposalC <- raftpb.ConfChange{
		Type:   raftpb.ConfChangeAddNode,
		NodeID: raftId,
	}
	return true, nil
}

//
// MsgWriter interface (necessary for p2p.Send)
//

func (pm *ProtocolManager) WriteMsg(msg p2p.Msg) error {
	// read *into* buffer
	var buffer = make([]byte, msg.Size)
	msg.Payload.Read(buffer)

	return pm.rawNode().Propose(context.TODO(), buffer)
}

//
// Raft interface
//

func (pm *ProtocolManager) Process(ctx context.Context, m raftpb.Message) error {
	return pm.rawNode().Step(ctx, m)
}

func (pm *ProtocolManager) IsIDRemoved(raftId uint64) bool {
	isRaftIdRemoved := pm.isRaftIdRemoved(raftId)
	//log.Info("checking if raft id removed?", "raftId", raftId, "isRaftIdRemoved", isRaftIdRemoved)
	return isRaftIdRemoved
}

func (pm *ProtocolManager) ReportUnreachable(id uint64) {
	log.Info("peer is currently unreachable", "peer id", id)

	pm.rawNode().ReportUnreachable(id)
}

func (pm *ProtocolManager) ReportSnapshot(id uint64, status etcdRaft.SnapshotStatus) {
	if status == etcdRaft.SnapshotFailure {
		log.Info("failed to send snapshot", "raft peer", id)
	} else if status == etcdRaft.SnapshotFinish {
		log.Info("finished sending snapshot", "raft peer", id)
	}

	pm.rawNode().ReportSnapshot(id, status)
}

//
// Private methods
//

func (pm *ProtocolManager) startRaft() {
	if !fileutil.Exist(pm.snapdir) {
		if err := os.Mkdir(pm.snapdir, 0750); err != nil {
			fatalf("cannot create dir for snapshot (%v)", err)
		}
	}
	walExisted := wal.Exist(pm.waldir)
	lastAppliedIndex := pm.loadAppliedIndex()

	id := raftTypes.ID(pm.raftId).String()
	ss := stats.NewServerStats(id, id)
	log.Info("startRaft()", "raft id", id)
	pm.transport = &rafthttp.Transport{
		ID:          raftTypes.ID(pm.raftId),
		ClusterID:   0x1000,
		Raft:        pm,
		ServerStats: ss,
		LeaderStats: stats.NewLeaderStats(strconv.Itoa(int(pm.raftId))),
		ErrorC:      make(chan error),
	}
	pm.transport.Start()

	// We load the snapshot to connect to prev peers before replaying the WAL,
	// which typically goes further into the future than the snapshot.

	var maybeRaftSnapshot *raftpb.Snapshot
	if walExisted {
		maybeRaftSnapshot = pm.loadSnapshot() // re-establishes peer connections
	}

	loadedWal, entries := pm.replayWAL(maybeRaftSnapshot)
	pm.wal = loadedWal

	if walExisted {

		// If we shutdown but didn't manage to flush the state to disk, then it will be the case that we will only sync
		// up to the snapshot. In this case, we can replay the raft entries that we have in saved to replay the blocks
		// back into our chain. We output errors but cannot do much if one occurs, since we can't fork to a different
		// chain and all other nodes in the network have confirmed these blocks
		if maybeRaftSnapshot != nil {
			currentChainHead := pm.blockchain.CurrentBlock().Number()
			for _, entry := range entries {
				if entry.Type == raftpb.EntryNormal {
					var block types.Block
					if err := rlp.DecodeBytes(entry.Data, &block); err != nil {
						log.Error("error decoding block: ", "err", err)
						continue
					}

					if thisBlockHead := pm.blockchain.GetBlockByHash(block.Hash()); thisBlockHead != nil {
						// check if the block is already existing in the local chain
						// and the block number is greater than current chain head
						if thisBlockHeadNum := thisBlockHead.Number(); thisBlockHeadNum.Cmp(currentChainHead) > 0 {
							// insert the block only if its already seen
							blocks := []*types.Block{&block}
							if _, err := pm.blockchain.InsertChain(blocks); err != nil {
								log.Error("error inserting the block into the chain", "number", block.NumberU64(), "hash", block.Hash(), "err", err)
							}
						}
					}
				}
			}
		}

		if hardState, _, err := pm.raftStorage.InitialState(); err != nil {
			panic(fmt.Sprintf("failed to read initial state from raft while restarting: %v", err))
		} else {
			if lastPersistedCommittedIndex := hardState.Commit; lastPersistedCommittedIndex < lastAppliedIndex {
				log.Info("rolling back applied index to last-durably-committed", "last applied index", lastAppliedIndex, "last persisted index", lastPersistedCommittedIndex)

				// Roll back our applied index. See the logic and explanation around
				// the single call to `pm.applyNewChainHead` for more context.
				lastAppliedIndex = lastPersistedCommittedIndex
			}

			// fix raft applied index out of range
			firstIndex, err := pm.raftStorage.FirstIndex()
			if err != nil {
				panic(fmt.Sprintf("failed to read last persisted applied index from raft while restarting: %v", err))
			}
			lastPersistedAppliedIndex := firstIndex - 1
			if lastPersistedAppliedIndex > lastAppliedIndex {
				log.Debug("set lastAppliedIndex to lastPersistedAppliedIndex", "last applied index", lastAppliedIndex, "last persisted applied index", lastPersistedAppliedIndex)

				lastAppliedIndex = lastPersistedAppliedIndex
				pm.advanceAppliedIndex(lastAppliedIndex)
			}
		}
	}

	// NOTE: cockroach sets this to false for now until they've "worked out the
	//       bugs"
	enablePreVote := true

	raftConfig := &etcdRaft.Config{
		Applied:       lastAppliedIndex,
		ID:            pm.raftId,
		ElectionTick:  10, // NOTE: cockroach sets this to 15
		HeartbeatTick: 1,  // NOTE: cockroach sets this to 5
		Storage:       pm.raftStorage,

		// NOTE, from cockroach:
		// "PreVote and CheckQuorum are two ways of achieving the same thing.
		// PreVote is more compatible with quiesced ranges, so we want to switch
		// to it once we've worked out the bugs."
		//
		// TODO: vendor again?
		// PreVote:     enablePreVote,
		CheckQuorum: !enablePreVote,

		// MaxSizePerMsg controls how many Raft log entries the leader will send to
		// followers in a single MsgApp.
		MaxSizePerMsg: 4096, // NOTE: in cockroachdb this is 16*1024

		// MaxInflightMsgs controls how many in-flight messages Raft will send to
		// a follower without hearing a response. The total number of Raft log
		// entries is a combination of this setting and MaxSizePerMsg.
		//
		// NOTE: Cockroach's settings (MaxSizePerMsg of 4k and MaxInflightMsgs
		// of 4) provide for up to 64 KB of raft log to be sent without
		// acknowledgement. With an average entry size of 1 KB that translates
		// to ~64 commands that might be executed in the handling of a single
		// etcdraft.Ready operation.
		MaxInflightMsgs: 256, // NOTE: in cockroachdb this is 4
	}

	log.Info("startRaft", "raft ID", raftConfig.ID)

	if walExisted {
		log.Info("remounting an existing raft log; connecting to peers.")

		pm.unsafeRawNode = etcdRaft.RestartNode(raftConfig)
	} else if pm.joinExisting {
		log.Info("newly joining an existing cluster; waiting for connections.")
		pm.unsafeRawNode = etcdRaft.StartNode(raftConfig, nil)
	} else { // a new peer, either part of the initial the network set, or a node is joining an existing network. Note: all nodes must be present in the static-nodes.json
		log.Info("peer is joining the cluster.")
		// when joining the cluster, the static-nodes.json is the source or truth and represents the raft initial peer list.
		if numPeers := len(pm.bootstrapNodes); numPeers == 0 {
			panic("exiting due to empty raft peers list")
		} else {
			log.Info("starting a new raft log", "initial cluster size of", numPeers)
		}

		raftPeers, peerAddresses, localAddress := pm.makeInitialRaftPeers()

		pm.setLocalAddress(localAddress)

		// We add all peers up-front even though we will see a ConfChangeAddNode
		// for each shortly. This is because raft's ConfState will contain all of
		// these nodes before we see these log entries, and we always want our
		// snapshots to have all addresses for each of the nodes in the ConfState.
		log.Info("cluster size from static-nodes.json", "number of peers", len(peerAddresses))
		for _, peerAddress := range peerAddresses {
			log.Info("adding peer address to raft cluster", "peerAddress", peerAddress)
			pm.addPeer(peerAddress)
		}
		pm.unsafeRawNode = etcdRaft.StartNode(raftConfig, raftPeers)
	}
	log.Info("raft node started")
	go pm.serveRaft()
	go pm.serveLocalProposals()
	go pm.eventLoop()
	go pm.handleRoleChange(pm.rawNode().RoleChan().Out())
}

func (pm *ProtocolManager) setLocalAddress(addr *Address) {
	pm.mu.Lock()
	pm.address = addr
	pm.mu.Unlock()
	// By setting `URLs` on the raft transport, we advertise our URL (in an HTTP
	// header) to any recipient. This is necessary for a newcomer to the cluster
	// to be able to accept a snapshot from us to bootstrap them.
	if urls, err := raftTypes.NewURLs([]string{pm.raftUrl(addr)}); err == nil {
		pm.transport.URLs = urls
	} else {
		panic(fmt.Sprintf("error: could not create URL from local address: %v", addr))
	}
}

func (pm *ProtocolManager) serveRaft() {
	urlString := fmt.Sprintf("http://0.0.0.0:%d", pm.raftPort)
	url, err := url.Parse(urlString)
	if err != nil {
		fatalf("Failed parsing URL (%v)", err)
	}

	listener, err := newStoppableListener(url.Host, pm.httpstopc)
	if err != nil {
		fatalf("Failed to listen rafthttp (%v)", err)
	}
	err = (&http.Server{Handler: pm.transport.Handler()}).Serve(listener)
	select {
	case <-pm.httpstopc:
	default:
		fatalf("Failed to serve rafthttp (%v)", err)
	}
	close(pm.httpdonec)
}

func (pm *ProtocolManager) isLearner(rid uint64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, n := range pm.confState.Learners {
		if n == rid {
			return true
		}
	}
	return false
}

func (pm *ProtocolManager) isLearnerNode() bool {
	return pm.isLearner(pm.raftId)
}

func (pm *ProtocolManager) isVerifierNode() bool {
	return pm.isVerifier(pm.raftId)
}

func (pm *ProtocolManager) isVerifier(rid uint64) bool {
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	for _, n := range pm.confState.Nodes {
		if n == rid {
			return true
		}
	}
	return false
}

func (pm *ProtocolManager) handleRoleChange(roleC <-chan interface{}) {
	for {
		select {
		case role := <-roleC:
			intRole, ok := role.(int)

			if !ok {
				panic("Couldn't cast role to int")
			}
			if intRole == minterRole {
				log.EmitCheckpoint(log.BecameMinter)
				pm.minter.start()
			} else { // verifier
				if pm.isVerifierNode() {
					log.EmitCheckpoint(log.BecameVerifier)
				} else {
					log.EmitCheckpoint(log.BecameLearner)
				}
				pm.minter.stop()
			}

			pm.mu.Lock()
			pm.role = intRole
			pm.mu.Unlock()
		case <-pm.quitSync:
			return
		}
	}
}

func (pm *ProtocolManager) minedBroadcastLoop() {
	for obj := range pm.minedBlockSub.Chan() {
		switch ev := obj.Data.(type) {
		case core.NewMinedBlockEvent:
			select {
			case pm.blockProposalC <- ev.Block:
			case <-pm.quitSync:
				return
			}
		}
	}
}

// Serve two channels to handle new blocks and raft configuration changes originating locally.
func (pm *ProtocolManager) serveLocalProposals() {
	//
	// TODO: does it matter that this will restart from 0 whenever we restart a cluster?
	//
	var confChangeCount uint64

	for {
		select {
		case block, ok := <-pm.blockProposalC:
			if !ok {
				log.Info("error: read from blockProposalC failed")
				return
			}

			size, r, err := rlp.EncodeToReader(block)
			if err != nil {
				panic(fmt.Sprintf("error: failed to send RLP-encoded block: %s", err.Error()))
			}
			var buffer = make([]byte, uint32(size))
			r.Read(buffer)

			// blocks until accepted by the raft state machine
			pm.rawNode().Propose(context.TODO(), buffer)
		case cc, ok := <-pm.confChangeProposalC:
			if !ok {
				log.Info("error: read from confChangeProposalC failed")
				return
			}

			confChangeCount++
			cc.ID = confChangeCount
			pm.rawNode().ProposeConfChange(context.TODO(), cc)
		case <-pm.quitSync:
			return
		}
	}
}

func (pm *ProtocolManager) entriesToApply(allEntries []raftpb.Entry) (entriesToApply []raftpb.Entry) {
	if len(allEntries) == 0 {
		return
	}

	first := allEntries[0].Index
	pm.mu.RLock()
	lastApplied := pm.appliedIndex
	pm.mu.RUnlock()

	if first > lastApplied+1 {
		fatalf("first index of committed entry[%d] should <= appliedIndex[%d] + 1", first, lastApplied)
	}

	firstToApply := lastApplied - first + 1

	if firstToApply < uint64(len(allEntries)) {
		entriesToApply = allEntries[firstToApply:]
	}
	return
}

func (pm *ProtocolManager) raftUrl(address *Address) string {
	if parsedIp := net.ParseIP(address.Hostname); parsedIp != nil {
		if ipv4 := parsedIp.To4(); ipv4 != nil {
			//this is an IPv4 address
			return fmt.Sprintf("http://%s:%d", ipv4, address.RaftPort)
		}
		//this is an IPv6 address
		return fmt.Sprintf("http://[%s]:%d", parsedIp, address.RaftPort)
	}
	return fmt.Sprintf("http://%s:%d", address.Hostname, address.RaftPort)
}

func (pm *ProtocolManager) addPeer(address *Address) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	raftId := address.RaftId

	//Quorum - RAFT - derive pubkey from nodeId
	pubKey, err := enode.HexPubkey(address.NodeId.String())
	if err != nil {
		log.Error("error decoding pub key from enodeId", "enodeId", address.NodeId.String(), "err", err)
		panic(err)
	}

	// Add P2P connection:
	p2pNode := enode.NewV4Hostname(pubKey, address.Hostname, int(address.P2pPort), 0, int(address.RaftPort))
	log.Info("Adding raft p2p peer", "pm.p2pServer.AddPeer", p2pNode)
	pm.p2pServer.AddPeer(p2pNode)

	// Add raft transport connection:
	pm.transport.AddPeer(raftTypes.ID(raftId), []string{pm.raftUrl(address)})
	pm.peers[uint64(raftId)] = &Peer{address, p2pNode}
}

func (pm *ProtocolManager) disconnectFromPeer(raftId uint64, peer *Peer) {
	pm.p2pServer.RemovePeer(peer.p2pNode)
	pm.transport.RemovePeer(raftTypes.ID(raftId))
}

func (pm *ProtocolManager) removePeer(raftId uint64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if peer := pm.peers[raftId]; peer != nil {
		pm.disconnectFromPeer(raftId, peer)

		delete(pm.peers, raftId)
	}

	// This is only necessary sometimes, but it's idempotent. Also, we *always*
	// do this, and not just when there's still a peer in the map, because we
	// need to do it for our *own* raft ID before we get booted from the cluster
	// so that snapshots are identical on all nodes. It's important for a booted
	// node to have a snapshot identical to every other node because that node
	// can potentially re-enter the cluster with a new raft ID.
	pm.removedPeers.Add(raftId)
}

func (pm *ProtocolManager) eventLoop() {
	ticker := time.NewTicker(tickerMS * time.Millisecond)
	defer ticker.Stop()
	defer pm.wal.Close()

	exitAfterApplying := false

	for {
		select {
		case <-ticker.C:
			pm.rawNode().Tick()

			// when the node is first ready it gives us entries to commit and messages
			// to immediately publish
		case rd := <-pm.rawNode().Ready():
			pm.wal.Save(rd.HardState, rd.Entries)

			if rd.SoftState != nil {
				pm.updateLeader(rd.SoftState.Lead)
			}

			if snap := rd.Snapshot; !etcdRaft.IsEmptySnap(snap) {
				pm.saveRaftSnapshot(snap)
				pm.applyRaftSnapshot(snap)
				pm.advanceAppliedIndex(snap.Metadata.Index)
			}

			// 1: Write HardState, Entries, and Snapshot to persistent storage if they
			// are not empty.
			pm.raftStorage.Append(rd.Entries)

			// 2: Send all Messages to the nodes named in the To field.
			pm.transport.Send(rd.Messages)

			// 3: Apply Snapshot (if any) and CommittedEntries to the state machine.
			for _, entry := range pm.entriesToApply(rd.CommittedEntries) {
				switch entry.Type {
				case raftpb.EntryNormal:
					if len(entry.Data) == 0 {
						break
					}
					var block types.Block
					err := rlp.DecodeBytes(entry.Data, &block)
					if err != nil {
						log.Error("error decoding block", "err", err)
					}

					if pm.blockchain.HasBlock(block.Hash(), block.NumberU64()) {
						// This can happen:
						//
						// if (1) we crashed after applying this block to the chain, but
						//        before writing appliedIndex to LDB.
						// or (2) we crashed in a scenario where we applied further than
						//        raft *durably persisted* its committed index (see
						//        https://github.com/coreos/etcd/pull/7899). In this
						//        scenario, when the node comes back up, we will re-apply
						//        a few entries.

						headBlockHash := pm.blockchain.CurrentBlock().Hash()
						log.Warn("not applying already-applied block", "block hash", block.Hash(), "parent", block.ParentHash(), "head", headBlockHash)
					} else {
						if !pm.applyNewChainHead(&block) {
							// return false only if insert chain is interrupted
							// stop eventloop
							return
						}
					}

				case raftpb.EntryConfChange:
					var cc raftpb.ConfChange
					cc.Unmarshal(entry.Data)
					raftId := cc.NodeID

					pm.confState = *pm.rawNode().ApplyConfChange(cc)
					log.Info("confChange", "confState", pm.confState)
					forceSnapshot := false

					switch cc.Type {
					case raftpb.ConfChangeAddNode, raftpb.ConfChangeAddLearnerNode:
						confChangeTypeName := raftpb.ConfChangeType_name[int32(cc.Type)]
						log.Info(confChangeTypeName, "raft id", raftId)

						// We need a snapshot to exist to reconnect to peers on start-up after a crash.
						forceSnapshot = true

						// If the peer being added or promoted is in the removed list, delete it from the list as it being added back.
						if pm.isRaftIdRemoved(raftId) {
							log.Info("received config change "+confChangeTypeName+
								"for a previously removed node, this is an attempt to add the peer back, remove the peer "+
								"from removed list", "raftId", raftId, "pm.removedPeers", pm.removedPeers)
							pm.removedPeers.Remove(raftId)
						}
						// if we know this peer already or it is this peer, the state is either in the initial configuration
						// (See initial cluster logic in startRaft() for more information), or it is a command to
						//  promote a learner node to a voter node (the raft core protocol will take care of the promotion).
						if pm.isRaftIdUsed(raftId) {
							log.Info("either ignoring expected confChange for (initial) peer, or promoting learner node to voter node ",
								"confChangeTypeName", confChangeTypeName, "raft id", raftId)
						} else {
							// if the raft id does not exist as a peer, then adding a new peer/learner
							// (either for the first time, or it was previously removed) to an existing network.
							log.Info("add peer/learner -> "+confChangeTypeName, "raft id", raftId)
							// TODO: fix this in 2.6 as it can crash all the nodes.
							pm.addPeer(bytesToAddress(cc.Context))
						}

					case raftpb.ConfChangeRemoveNode:
						if pm.isRaftIdRemoved(raftId) {
							log.Info("ignoring ConfChangeRemoveNode for already-removed peer", "raft id", raftId)
						} else {
							log.Info("removing peer due to ConfChangeRemoveNode", "raft id", raftId)

							forceSnapshot = true

							if raftId == pm.raftId {
								exitAfterApplying = true
							}

							pm.removePeer(raftId)
						}

					case raftpb.ConfChangeUpdateNode:
						// NOTE: remember to forceSnapshot in this case, if we add support
						// for this.
						fatalf("not yet handled: ConfChangeUpdateNode")
					}

					if forceSnapshot {
						// We force a snapshot here to persist our updated confState, so we
						// know our fellow cluster members when we come back online.
						//
						// It is critical here to snapshot *before* writing our applied
						// index in LevelDB, otherwise a crash while/before snapshotting
						// (after advancing our applied index) would result in the loss of a
						// cluster member upon restart: we would re-mount with an old
						// ConfState.
						pm.triggerSnapshot(entry.Index)
					}
				}

				pm.advanceAppliedIndex(entry.Index)
			}

			pm.maybeTriggerSnapshot()

			if exitAfterApplying {
				log.Warn("permanently removing self from the cluster")
				pm.Stop()
				log.Warn("permanently exited the cluster")

				return
			}

			// 4: Call Node.Advance() to signal readiness for the next batch of
			// updates.
			pm.rawNode().Advance()

		case <-pm.quitSync:
			return
		}
	}
}

func (pm *ProtocolManager) makeInitialRaftPeers() (raftPeers []etcdRaft.Peer, peerAddresses []*Address, localAddress *Address) {
	initialNodes := pm.bootstrapNodes
	raftPeers = make([]etcdRaft.Peer, len(initialNodes))  // Entire cluster
	peerAddresses = make([]*Address, len(initialNodes)-1) // Cluster without *this* node

	peersSeen := 0
	for i, node := range initialNodes {
		raftId, err := nodeIdToRaftId(node.EnodeID())
		if err != nil {
			log.Error("Error converting enodeId to uint64 raftId", "enodeId", node.EnodeID(), "err", err)
			panic(fmt.Sprintf("Error converting enodeId to uint64 raftId. enode id is [%v] error is [%v]", node.EnodeID(), err))
		}
		log.Info("Obtained raft peer", "raftId", raftId, "nodeId", node.ID().String())
		// We initially get the raftPort from the enode ID's query string. As an alternative, we can move away from
		// requiring the use of static peers for the initial set, and load them from e.g. another JSON file which
		// contains pairs of enodes and raft ports, or we can get this initial peer list from commandline flags.
		address := newAddress(raftId, node.RaftPort(), node, pm.useDns)
		raftPeers[i] = etcdRaft.Peer{
			ID:      uint64(raftId),
			Context: address.toBytes(),
		}

		if uint64(raftId) == pm.raftId {
			localAddress = address
		} else {
			peerAddresses[peersSeen] = address
			peersSeen += 1
		}
	}

	return raftPeers, peerAddresses, localAddress
}

func blockExtendsChain(block *types.Block, chain *core.BlockChain) bool {
	return block.ParentHash() == chain.CurrentBlock().Hash()
}

func (pm *ProtocolManager) applyNewChainHead(block *types.Block) bool {
	if !blockExtendsChain(block, pm.blockchain) {
		headBlock := pm.blockchain.CurrentBlock()

		log.Info("Non-extending block", "block", block.Hash(), "parent", block.ParentHash(), "head", headBlock.Hash())

		pm.minter.invalidRaftOrderingChan <- InvalidRaftOrdering{headBlock: headBlock, invalidBlock: block}
	} else {
		if existingBlock := pm.blockchain.GetBlockByHash(block.Hash()); nil == existingBlock {
			if err := pm.blockchain.Validator().ValidateBody(block); err != nil {
				panic(fmt.Sprintf("failed to validate block %x (%v)", block.Hash(), err))
			}
		}

		for _, tx := range block.Transactions() {
			log.EmitCheckpoint(log.TxAccepted, "tx", tx.Hash().Hex())
		}

		_, err := pm.blockchain.InsertChain([]*types.Block{block})

		if err != nil {
			if err == core.ErrAbortBlocksProcessing {
				log.Error(fmt.Sprintf("failed to extend chain: %s", err.Error()))
				return false
			}
			panic(fmt.Sprintf("failed to extend chain: %s", err.Error()))
		}

		log.EmitCheckpoint(log.BlockCreated, "block", fmt.Sprintf("%x", block.Hash()))
	}
	return true
}

// Sets new appliedIndex in-memory, *and* writes this appliedIndex to LevelDB.
func (pm *ProtocolManager) advanceAppliedIndex(index uint64) {
	pm.writeAppliedIndex(index)

	pm.mu.Lock()
	pm.appliedIndex = index
	pm.mu.Unlock()
}

func (pm *ProtocolManager) updateLeader(leaderRaftId uint64) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.leader = leaderRaftId
}

// The Address for the current leader, or an error if no leader is elected.
func (pm *ProtocolManager) LeaderAddress() (*Address, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if minterRole == pm.role {
		return pm.address, nil
	} else if l, ok := pm.peers[pm.leader]; ok {
		return l.address, nil
	}
	// We expect to reach this if pm.leader is 0, which is how etcd denotes the lack of a leader.
	return nil, errNoLeaderElected
}

// Returns the raft id for a given enodeId
func (pm *ProtocolManager) FetchRaftId(enodeId string) (uint64, error) {
	node, err := enode.ParseV4(enodeId)
	if err != nil {
		return 0, err
	}
	for raftId, peer := range pm.peers {
		if peer.p2pNode.ID() == node.ID() {
			return raftId, nil
		}
	}
	return 0, fmt.Errorf("node not found in the cluster: %v", enodeId)
}
