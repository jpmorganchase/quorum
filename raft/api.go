package raft

import (
	"errors"
	"strconv"

	"github.com/coreos/etcd/pkg/types"
)

type RaftNodeInfo struct {
	ClusterSize    int        `json:"clusterSize"`
	Role           string     `json:"role"`
	Address        *Address   `json:"address"`
	PeerAddresses  []*Address `json:"peerAddresses"`
	RemovedPeerIds []uint64   `json:"removedPeerIds"`
	AppliedIndex   uint64     `json:"appliedIndex"`
	SnapshotIndex  uint64     `json:"snapshotIndex"`
}

type PublicRaftAPI struct {
	raftService *RaftService
}

func NewPublicRaftAPI(raftService *RaftService) *PublicRaftAPI {
	return &PublicRaftAPI{raftService}
}

func (s *PublicRaftAPI) Role() string {
	if err := s.checkIfNodeInCluster(); err != nil {
		return ""
	}
	_, err := s.raftService.raftProtocolManager.LeaderAddress()
	if err != nil {
		return ""
	}
	return s.raftService.raftProtocolManager.NodeInfo().Role
}

// helper function to check if self node is part of cluster
func (s *PublicRaftAPI) checkIfNodeInCluster() error {
	if s.raftService.raftProtocolManager.IsIDRemoved(s.raftService.raftProtocolManager.raftId) {
		return errors.New("node not part of raft cluster. operations not allowed")
	}
	return nil
}

func (s *PublicRaftAPI) AddPeer(enodeId string) (string, error) {
	if err := s.checkIfNodeInCluster(); err != nil {
		return "0", err
	}
	raftIdUint64, err := s.raftService.raftProtocolManager.ProposeNewPeer(enodeId, false)
	raftId := RaftIdToString(raftIdUint64)
	return raftId, err
}

func (s *PublicRaftAPI) AddLearner(enodeId string) (string, error) {
	if err := s.checkIfNodeInCluster(); err != nil {
		return "0", err
	}
	raftIdUint64, err := s.raftService.raftProtocolManager.ProposeNewPeer(enodeId, true)
	raftId := RaftIdToString(raftIdUint64)
	return raftId, err
}

func (s *PublicRaftAPI) PromoteToPeer(raftIdStr string) (bool, error) {
	if err := s.checkIfNodeInCluster(); err != nil {
		return false, err
	}
	raftId, err := strconv.ParseUint(raftIdStr, 10, 64)
	if err != nil {
		return false, err
	}
	return s.raftService.raftProtocolManager.PromoteToPeer(raftId)
}

func (s *PublicRaftAPI) RemovePeer(raftIdStr string) error {
	if err := s.checkIfNodeInCluster(); err != nil {
		return err
	}
	raftId, err := strconv.ParseUint(raftIdStr, 10, 64)
	if err != nil {
		return err
	}
	return s.raftService.raftProtocolManager.ProposePeerRemoval(raftId)
}

func (s *PublicRaftAPI) Leader() (string, error) {

	addr, err := s.raftService.raftProtocolManager.LeaderAddress()
	if err != nil {
		return "", err
	}
	return addr.NodeId.String(), nil
}

func (s *PublicRaftAPI) Cluster() ([]ClusterInfo, error) {
	// check if the node has already been removed from cluster
	// if yes return nil
	if err := s.checkIfNodeInCluster(); err != nil {
		return []ClusterInfo{}, nil
	}

	nodeInfo := s.raftService.raftProtocolManager.NodeInfo()
	if nodeInfo.Role == "" {
		return []ClusterInfo{}, nil
	}

	noLeader := false
	leaderAddr, err := s.raftService.raftProtocolManager.LeaderAddress()
	if err != nil {
		noLeader = true
		if s.raftService.raftProtocolManager.NodeInfo().Role == "" {
			return []ClusterInfo{}, nil
		}
	}

	peerAddresses := append(nodeInfo.PeerAddresses, nodeInfo.Address)
	clustInfo := make([]ClusterInfo, len(peerAddresses))
	for i, a := range peerAddresses {
		// used to display the address in JS, raftId in Javascript is represented as a String as a JS Number and uint64 are not compatible.
		aJS := newAddressJS(a)
		role := ""
		if !noLeader {
			if a.RaftId == leaderAddr.RaftId {
				role = "minter"
			} else if s.raftService.raftProtocolManager.isLearner(uint64(a.RaftId)) {
				role = "learner"
			} else if s.raftService.raftProtocolManager.isVerifier(uint64(a.RaftId)) {
				role = "verifier"
			}
		}
		//log.Info("Cluster RaftId is: ", "a.RaftId", a.RaftId, "aJS.RaftId", aJS.RaftId)
		clustInfo[i] = ClusterInfo{*aJS, role, s.checkIfNodeIsActive(uint64(a.RaftId))}
	}
	return clustInfo, nil
}

// checkIfNodeIsActive checks if the raft node is active
// if the raft node is active ActiveSince returns non-zero time
func (s *PublicRaftAPI) checkIfNodeIsActive(raftId uint64) bool {
	if raftId == s.raftService.raftProtocolManager.raftId {
		return true
	}
	activeSince := s.raftService.raftProtocolManager.transport.ActiveSince(types.ID(raftId))
	return !activeSince.IsZero()
}

func (s *PublicRaftAPI) GetRaftId(enodeId string) (uint64, error) {
	return s.raftService.raftProtocolManager.FetchRaftId(enodeId)
}
