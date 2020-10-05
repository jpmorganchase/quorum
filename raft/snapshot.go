package raft

import (
	"bytes"
	"fmt"
	"io"
	"math/big"
	"net"
	"sort"
	"time"

	"github.com/coreos/etcd/raft/raftpb"
	"github.com/coreos/etcd/snap"
	"github.com/coreos/etcd/wal/walpb"
	mapset "github.com/deckarep/golang-set"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/p2p/enode"
	"github.com/ethereum/go-ethereum/p2p/enr"
	"github.com/ethereum/go-ethereum/rlp"
)

type SnapshotWithHostnames struct {
	Addresses      []Address
	RemovedRaftIds []uint64
	HeadBlockHash  common.Hash
}

type AddressWithoutHostname struct {
	RaftId   uint16
	NodeId   enode.EnodeID
	Ip       net.IP
	P2pPort  enr.TCP
	RaftPort enr.RaftPort
}

type SnapshotWithoutHostnames struct {
	Addresses      []AddressWithoutHostname
	RemovedRaftIds []uint64 // Raft IDs for removed peers, peers can be added back if they have been removed.
	HeadBlockHash  common.Hash
}

type ByRaftId []Address

func (a ByRaftId) Len() int           { return len(a) }
func (a ByRaftId) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a ByRaftId) Less(i, j int) bool { return a[i].RaftId < a[j].RaftId }

func (pm *ProtocolManager) buildSnapshot() *SnapshotWithHostnames {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	numNodes := len(pm.confState.Nodes) + len(pm.confState.Learners)
	numRemovedNodes := pm.removedPeers.Cardinality()
	snapshot := &SnapshotWithHostnames{
		Addresses:      make([]Address, numNodes),
		RemovedRaftIds: make([]uint64, numRemovedNodes),
		HeadBlockHash:  pm.blockchain.CurrentBlock().Hash(),
	}

	// Populate addresses
	for i, rawRaftId := range append(pm.confState.Nodes, pm.confState.Learners...) {
		raftId := rawRaftId

		if raftId == pm.raftId {
			snapshot.Addresses[i] = *pm.address
		} else {
			snapshot.Addresses[i] = *pm.peers[raftId].address
		}
	}
	sort.Sort(ByRaftId(snapshot.Addresses))

	// Populate removed IDs
	i := 0
	for removedIface := range pm.removedPeers.Iterator().C {
		snapshot.RemovedRaftIds[i] = removedIface.(uint64)
		i++
	}
	return snapshot
}

// Note that we do *not* read `pm.appliedIndex` here. We only use the `index`
// parameter instead. This is because we need to support a scenario when we
// snapshot for a future index that we have not yet recorded in LevelDB. See
// comments around the use of `forceSnapshot`.
func (pm *ProtocolManager) triggerSnapshot(index uint64) {
	pm.mu.RLock()
	snapshotIndex := pm.snapshotIndex
	pm.mu.RUnlock()

	log.Info("start snapshot", "applied index", pm.appliedIndex, "last snapshot index", snapshotIndex)

	//snapData := pm.blockchain.CurrentBlock().Hash().Bytes()
	//snap, err := pm.raftStorage.CreateSnapshot(pm.appliedIndex, &pm.confState, snapData)
	snapData := pm.buildSnapshot().toBytes()
	snap, err := pm.raftStorage.CreateSnapshot(index, &pm.confState, snapData)
	if err != nil {
		panic(err)
	}
	if err := pm.saveRaftSnapshot(snap); err != nil {
		panic(err)
	}
	// Discard all log entries prior to index.
	if err := pm.raftStorage.Compact(index); err != nil {
		panic(err)
	}
	log.Info("compacted log", "index", pm.appliedIndex)

	pm.mu.Lock()
	pm.snapshotIndex = index
	pm.mu.Unlock()
}

func confStateIdSet(confState raftpb.ConfState) mapset.Set {
	set := mapset.NewSet()
	for _, rawRaftId := range append(confState.Nodes, confState.Learners...) {
		set.Add(rawRaftId)
	}
	return set
}

func (pm *ProtocolManager) updateClusterMembership(newConfState raftpb.ConfState, addresses []Address, removedRaftIds []uint64) {
	log.Info("updating cluster membership per raft snapshot")

	prevConfState := pm.confState

	// Update tombstones for permanently removed peers. For simplicity we do not
	// allow the re-use of peer IDs once a peer is removed.

	removedPeers := mapset.NewSet()
	for _, removedRaftId := range removedRaftIds {
		removedPeers.Add(removedRaftId)
	}
	pm.mu.Lock()
	pm.removedPeers = removedPeers
	pm.mu.Unlock()

	// Remove old peers that we're still connected to
	prevIds := confStateIdSet(prevConfState)
	newIds := confStateIdSet(newConfState)
	idsToRemove := prevIds.Difference(newIds)
	for idIfaceToRemove := range idsToRemove.Iterator().C {
		raftId := idIfaceToRemove.(uint64)
		log.Info("removing old raft peer", "peer id", raftId)

		pm.removePeer(raftId)
	}

	// Update local and remote addresses

	for _, tempAddress := range addresses {
		address := tempAddress // Allocate separately on the heap for each iteration.

		if uint64(address.RaftId) == pm.raftId {
			// If we're a newcomer to an existing cluster, this is where we learn
			// our own Address.
			pm.setLocalAddress(&address)
		} else {
			pm.mu.RLock()
			existingPeer := pm.peers[uint64(address.RaftId)]
			pm.mu.RUnlock()

			if existingPeer == nil {
				log.Info("adding new raft peer", "raft id", address.RaftId)
				pm.addPeer(&address)
			}
		}
	}

	pm.mu.Lock()
	pm.confState = newConfState
	pm.mu.Unlock()

	log.Info("updated cluster membership")
}

func (pm *ProtocolManager) maybeTriggerSnapshot() {
	pm.mu.RLock()
	appliedIndex := pm.appliedIndex
	entriesSinceLastSnap := appliedIndex - pm.snapshotIndex
	pm.mu.RUnlock()

	if entriesSinceLastSnap < snapshotPeriod {
		return
	}

	pm.triggerSnapshot(appliedIndex)
}

func (pm *ProtocolManager) loadSnapshot() *raftpb.Snapshot {
	if raftSnapshot := pm.readRaftSnapshot(); raftSnapshot != nil {
		log.Info("loading snapshot")
		pm.applyRaftSnapshot(*raftSnapshot)

		return raftSnapshot
	} else {
		log.Info("no snapshot to load")

		return nil
	}
}

func (snapshot *SnapshotWithHostnames) toBytes() []byte {
	var (
		useOldSnapshot bool
		oldSnapshot    SnapshotWithoutHostnames
		toEncode       interface{}
	)

	// use old snapshot if all snapshot.Addresses are ips
	// but use the new snapshot if any of it is a hostname
	useOldSnapshot = true
	oldSnapshot.HeadBlockHash, oldSnapshot.RemovedRaftIds = snapshot.HeadBlockHash, snapshot.RemovedRaftIds
	oldSnapshot.Addresses = make([]AddressWithoutHostname, len(snapshot.Addresses))

	for index, addrWithHost := range snapshot.Addresses {
		// validate addrWithHost.Hostname is a hostname/ip
		ip := net.ParseIP(addrWithHost.Hostname)
		if ip == nil {
			// this is a hostname
			useOldSnapshot = false
			break
		}
		// this is an ip
		oldSnapshot.Addresses[index] = AddressWithoutHostname{
			addrWithHost.RaftId,
			addrWithHost.NodeId,
			ip,
			addrWithHost.P2pPort,
			addrWithHost.RaftPort,
		}
	}

	if useOldSnapshot {
		toEncode = oldSnapshot
	} else {
		toEncode = snapshot
	}
	buffer, err := rlp.EncodeToBytes(toEncode)
	if err != nil {
		panic(fmt.Sprintf("error: failed to RLP-encode Snapshot: %s", err.Error()))
	}
	return buffer
}

func bytesToSnapshot(input []byte) *SnapshotWithHostnames {
	var err, errOld error

	snapshot := new(SnapshotWithHostnames)
	streamNewSnapshot := rlp.NewStream(bytes.NewReader(input), 0)
	if err = streamNewSnapshot.Decode(snapshot); err == nil {
		return snapshot
	}

	// Build new snapshot with hostname from legacy Address struct
	snapshotOld := new(SnapshotWithoutHostnames)
	streamOldSnapshot := rlp.NewStream(bytes.NewReader(input), 0)
	if errOld = streamOldSnapshot.Decode(snapshotOld); errOld == nil {
		var snapshotConverted SnapshotWithHostnames
		snapshotConverted.RemovedRaftIds, snapshotConverted.HeadBlockHash = snapshotOld.RemovedRaftIds, snapshotOld.HeadBlockHash
		snapshotConverted.Addresses = make([]Address, len(snapshotOld.Addresses))

		for index, oldAddrWithIp := range snapshotOld.Addresses {
			snapshotConverted.Addresses[index] = Address{
				RaftId:   oldAddrWithIp.RaftId,
				NodeId:   oldAddrWithIp.NodeId,
				Ip:       nil,
				P2pPort:  oldAddrWithIp.P2pPort,
				RaftPort: oldAddrWithIp.RaftPort,
				Hostname: oldAddrWithIp.Ip.String(),
			}
		}

		return &snapshotConverted
	}

	fatalf("failed to RLP-decode Snapshot: %v, %v", err, errOld)
	return nil
}

func (snapshot *SnapshotWithHostnames) EncodeRLP(w io.Writer) error {
	return rlp.Encode(w, []interface{}{snapshot.Addresses, snapshot.RemovedRaftIds, snapshot.HeadBlockHash})
}

// Raft snapshot

func (pm *ProtocolManager) saveRaftSnapshot(snap raftpb.Snapshot) error {
	if err := pm.snapshotter.SaveSnap(snap); err != nil {
		return err
	}

	walSnap := walpb.Snapshot{
		Index: snap.Metadata.Index,
		Term:  snap.Metadata.Term,
	}

	if err := pm.wal.SaveSnapshot(walSnap); err != nil {
		return err
	}

	return pm.wal.ReleaseLockTo(snap.Metadata.Index)
}

func (pm *ProtocolManager) readRaftSnapshot() *raftpb.Snapshot {
	snapshot, err := pm.snapshotter.Load()
	if err != nil && err != snap.ErrNoSnapshot {
		fatalf("error loading snapshot: %v", err)
	}

	return snapshot
}

func (pm *ProtocolManager) applyRaftSnapshot(raftSnapshot raftpb.Snapshot) {
	log.Info("applying snapshot to raft storage")
	if err := pm.raftStorage.ApplySnapshot(raftSnapshot); err != nil {
		fatalf("failed to apply snapshot: %s", err)
	}
	snapshot := bytesToSnapshot(raftSnapshot.Data)

	latestBlockHash := snapshot.HeadBlockHash

	pm.updateClusterMembership(raftSnapshot.Metadata.ConfState, snapshot.Addresses, snapshot.RemovedRaftIds)

	preSyncHead := pm.blockchain.CurrentBlock()

	if latestBlock := pm.blockchain.GetBlockByHash(latestBlockHash); latestBlock == nil {
		pm.syncBlockchainUntil(latestBlockHash)
		pm.logNewlyAcceptedTransactions(preSyncHead)

		log.Info(chainExtensionMessage, "hash", pm.blockchain.CurrentBlock().Hash())
	} else {
		// added for permissions changes to indicate node sync up has started
		types.SetSyncStatus()
		log.Info("blockchain is caught up; no need to synchronize")
	}

	snapMeta := raftSnapshot.Metadata
	pm.confState = snapMeta.ConfState
	pm.mu.Lock()
	pm.snapshotIndex = snapMeta.Index
	pm.mu.Unlock()
}

func (pm *ProtocolManager) syncBlockchainUntil(hash common.Hash) {
	pm.mu.RLock()
	peerMap := make(map[uint64]*Peer, len(pm.peers))
	for raftId, peer := range pm.peers {
		peerMap[raftId] = peer
	}
	pm.mu.RUnlock()

	for {
		for peerId, peer := range peerMap {
			log.Info("synchronizing with peer", "peer id", peerId, "hash", hash)

			peerId := peer.p2pNode.ID().String()
			peerIdPrefix := fmt.Sprintf("%x", peer.p2pNode.ID().Bytes()[:8])

			if err := pm.downloader.Synchronise(peerIdPrefix, hash, big.NewInt(0), downloader.BoundedFullSync); err != nil {
				log.Info("failed to synchronize with peer", "peer id", peerId)

				time.Sleep(500 * time.Millisecond)
			} else {
				return
			}
		}
	}
}

func (pm *ProtocolManager) logNewlyAcceptedTransactions(preSyncHead *types.Block) {
	newHead := pm.blockchain.CurrentBlock()
	numBlocks := newHead.NumberU64() - preSyncHead.NumberU64()
	blocks := make([]*types.Block, numBlocks)
	currBlock := newHead
	blocksSeen := 0
	for currBlock.Hash() != preSyncHead.Hash() {
		blocks[int(numBlocks)-(1+blocksSeen)] = currBlock

		blocksSeen += 1
		currBlock = pm.blockchain.GetBlockByHash(currBlock.ParentHash())
	}
	for _, block := range blocks {
		for _, tx := range block.Transactions() {
			log.EmitCheckpoint(log.TxAccepted, "tx", tx.Hash().Hex())
		}
	}
}
