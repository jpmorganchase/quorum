package eea

import (
	"fmt"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/node"
	binding "github.com/ethereum/go-ethereum/permission/eea/bind"
	ptype "github.com/ethereum/go-ethereum/permission/types"
)

type Backend struct {
	Node    *node.Node
	IsRaft  bool
	DataDir string
	Contr   *Contract
}

func (b *Backend) ManageAccountPermissions() error {
	chAccessModified := make(chan *binding.AcctManagerAccountAccessModified)
	chAccessRevoked := make(chan *binding.AcctManagerAccountAccessRevoked)
	chStatusChanged := make(chan *binding.AcctManagerAccountStatusChanged)

	opts := &bind.WatchOpts{}
	var blockNumber uint64 = 1
	opts.Start = &blockNumber

	if _, err := b.Contr.PermAcct.AcctManagerFilterer.WatchAccountAccessModified(opts, chAccessModified); err != nil {
		return fmt.Errorf("failed AccountAccessModified: %v", err)
	}

	if _, err := b.Contr.PermAcct.AcctManagerFilterer.WatchAccountAccessRevoked(opts, chAccessRevoked); err != nil {
		return fmt.Errorf("failed AccountAccessRevoked: %v", err)
	}

	if _, err := b.Contr.PermAcct.AcctManagerFilterer.WatchAccountStatusChanged(opts, chStatusChanged); err != nil {
		return fmt.Errorf("failed AccountStatusChanged: %v", err)
	}

	go func() {
		stopChan, stopSubscription := ptype.SubscribeStopEvent()
		defer stopSubscription.Unsubscribe()
		for {
			select {
			case evtAccessModified := <-chAccessModified:
				types.AcctInfoMap.UpsertAccount(evtAccessModified.OrgId, evtAccessModified.RoleId, evtAccessModified.Account, evtAccessModified.OrgAdmin, types.AcctStatus(int(evtAccessModified.Status.Uint64())))

			case evtAccessRevoked := <-chAccessRevoked:
				types.AcctInfoMap.UpsertAccount(evtAccessRevoked.OrgId, evtAccessRevoked.RoleId, evtAccessRevoked.Account, evtAccessRevoked.OrgAdmin, types.AcctActive)

			case evtStatusChanged := <-chStatusChanged:
				if ac, err := types.AcctInfoMap.GetAccount(evtStatusChanged.Account); ac != nil {
					types.AcctInfoMap.UpsertAccount(evtStatusChanged.OrgId, ac.RoleId, evtStatusChanged.Account, ac.IsOrgAdmin, types.AcctStatus(int(evtStatusChanged.Status.Uint64())))
				} else {
					log.Info("error fetching account information", "err", err)
				}
			case <-stopChan:
				log.Info("quit account contract watch")
				return
			}
		}
	}()
	return nil
}

func (b *Backend) ManageRolePermissions() error {
	chRoleCreated := make(chan *binding.RoleManagerRoleCreated, 1)
	chRoleRevoked := make(chan *binding.RoleManagerRoleRevoked, 1)

	opts := &bind.WatchOpts{}
	var blockNumber uint64 = 1
	opts.Start = &blockNumber

	if _, err := b.Contr.PermRole.RoleManagerFilterer.WatchRoleCreated(opts, chRoleCreated); err != nil {
		return fmt.Errorf("failed WatchRoleCreated: %v", err)
	}

	if _, err := b.Contr.PermRole.RoleManagerFilterer.WatchRoleRevoked(opts, chRoleRevoked); err != nil {
		return fmt.Errorf("failed WatchRoleRemoved: %v", err)
	}

	go func() {
		stopChan, stopSubscription := ptype.SubscribeStopEvent()
		defer stopSubscription.Unsubscribe()
		for {
			select {
			case evtRoleCreated := <-chRoleCreated:
				types.RoleInfoMap.UpsertRole(evtRoleCreated.OrgId, evtRoleCreated.RoleId, evtRoleCreated.IsVoter, evtRoleCreated.IsAdmin, types.AccessType(int(evtRoleCreated.BaseAccess.Uint64())), true)

			case evtRoleRevoked := <-chRoleRevoked:
				if r, _ := types.RoleInfoMap.GetRole(evtRoleRevoked.OrgId, evtRoleRevoked.RoleId); r != nil {
					types.RoleInfoMap.UpsertRole(evtRoleRevoked.OrgId, evtRoleRevoked.RoleId, r.IsVoter, r.IsAdmin, r.Access, false)
				} else {
					log.Error("Revoke role - cache is missing role", "org", evtRoleRevoked.OrgId, "role", evtRoleRevoked.RoleId)
				}
			case <-stopChan:
				log.Info("quit role contract watch")
				return
			}
		}
	}()
	return nil
}

func (b *Backend) ManageOrgPermissions() error {
	chPendingApproval := make(chan *binding.OrgManagerOrgPendingApproval, 1)
	chOrgApproved := make(chan *binding.OrgManagerOrgApproved, 1)
	chOrgSuspended := make(chan *binding.OrgManagerOrgSuspended, 1)
	chOrgReactivated := make(chan *binding.OrgManagerOrgSuspensionRevoked, 1)

	opts := &bind.WatchOpts{}
	var blockNumber uint64 = 1
	opts.Start = &blockNumber

	if _, err := b.Contr.PermOrg.OrgManagerFilterer.WatchOrgPendingApproval(opts, chPendingApproval); err != nil {
		return fmt.Errorf("failed WatchNodePendingApproval: %v", err)
	}

	if _, err := b.Contr.PermOrg.OrgManagerFilterer.WatchOrgApproved(opts, chOrgApproved); err != nil {
		return fmt.Errorf("failed WatchNodePendingApproval: %v", err)
	}

	if _, err := b.Contr.PermOrg.OrgManagerFilterer.WatchOrgSuspended(opts, chOrgSuspended); err != nil {
		return fmt.Errorf("failed WatchNodePendingApproval: %v", err)
	}

	if _, err := b.Contr.PermOrg.OrgManagerFilterer.WatchOrgSuspensionRevoked(opts, chOrgReactivated); err != nil {
		return fmt.Errorf("failed WatchNodePendingApproval: %v", err)
	}

	go func() {
		stopChan, stopSubscription := ptype.SubscribeStopEvent()
		defer stopSubscription.Unsubscribe()
		for {
			select {
			case evtPendingApproval := <-chPendingApproval:
				types.OrgInfoMap.UpsertOrg(evtPendingApproval.OrgId, evtPendingApproval.PorgId, evtPendingApproval.UltParent, evtPendingApproval.Level, types.OrgStatus(evtPendingApproval.Status.Uint64()))

			case evtOrgApproved := <-chOrgApproved:
				types.OrgInfoMap.UpsertOrg(evtOrgApproved.OrgId, evtOrgApproved.PorgId, evtOrgApproved.UltParent, evtOrgApproved.Level, types.OrgApproved)

			case evtOrgSuspended := <-chOrgSuspended:
				types.OrgInfoMap.UpsertOrg(evtOrgSuspended.OrgId, evtOrgSuspended.PorgId, evtOrgSuspended.UltParent, evtOrgSuspended.Level, types.OrgSuspended)

			case evtOrgReactivated := <-chOrgReactivated:
				types.OrgInfoMap.UpsertOrg(evtOrgReactivated.OrgId, evtOrgReactivated.PorgId, evtOrgReactivated.UltParent, evtOrgReactivated.Level, types.OrgApproved)
			case <-stopChan:
				log.Info("quit org contract watch")
				return
			}
		}
	}()
	return nil
}

func (b *Backend) ManageNodePermissions() error {
	chNodeApproved := make(chan *binding.NodeManagerNodeApproved, 1)
	chNodeProposed := make(chan *binding.NodeManagerNodeProposed, 1)
	chNodeDeactivated := make(chan *binding.NodeManagerNodeDeactivated, 1)
	chNodeActivated := make(chan *binding.NodeManagerNodeActivated, 1)
	chNodeBlacklisted := make(chan *binding.NodeManagerNodeBlacklisted)
	chNodeRecoveryInit := make(chan *binding.NodeManagerNodeRecoveryInitiated, 1)
	chNodeRecoveryDone := make(chan *binding.NodeManagerNodeRecoveryCompleted, 1)

	opts := &bind.WatchOpts{}
	var blockNumber uint64 = 1
	opts.Start = &blockNumber

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeApproved(opts, chNodeApproved); err != nil {
		return fmt.Errorf("failed WatchNodeApproved: %v", err)
	}

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeProposed(opts, chNodeProposed); err != nil {
		return fmt.Errorf("failed WatchNodeProposed: %v", err)
	}

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeDeactivated(opts, chNodeDeactivated); err != nil {
		return fmt.Errorf("failed NodeDeactivated: %v", err)
	}
	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeActivated(opts, chNodeActivated); err != nil {
		return fmt.Errorf("failed WatchNodeActivated: %v", err)
	}

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeBlacklisted(opts, chNodeBlacklisted); err != nil {
		return fmt.Errorf("failed NodeBlacklisting: %v", err)
	}

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeRecoveryInitiated(opts, chNodeRecoveryInit); err != nil {
		return fmt.Errorf("failed NodeRecoveryInitiated: %v", err)
	}

	if _, err := b.Contr.PermNode.NodeManagerFilterer.WatchNodeRecoveryCompleted(opts, chNodeRecoveryDone); err != nil {
		return fmt.Errorf("failed NodeRecoveryCompleted: %v", err)
	}

	go func() {
		stopChan, stopSubscription := ptype.SubscribeStopEvent()
		defer stopSubscription.Unsubscribe()
		for {
			select {
			case evtNodeApproved := <-chNodeApproved:
				enodeId := types.GetNodeUrl(evtNodeApproved.EnodeId, evtNodeApproved.Ip[:], evtNodeApproved.Port, evtNodeApproved.Raftport)
				err := ptype.UpdatePermissionedNodes(b.Node, b.DataDir, enodeId, ptype.NodeAdd, b.IsRaft)
				if err != nil {
					log.Error("error updating permissioned-nodes.json", "err", err)
				}
				types.NodeInfoMap.UpsertNode(evtNodeApproved.OrgId, enodeId, types.NodeApproved)

			case evtNodeProposed := <-chNodeProposed:
				enodeId := types.GetNodeUrl(evtNodeProposed.EnodeId, evtNodeProposed.Ip[:], evtNodeProposed.Port, evtNodeProposed.Raftport)
				types.NodeInfoMap.UpsertNode(evtNodeProposed.OrgId, enodeId, types.NodePendingApproval)

			case evtNodeDeactivated := <-chNodeDeactivated:
				enodeId := types.GetNodeUrl(evtNodeDeactivated.EnodeId, evtNodeDeactivated.Ip[:], evtNodeDeactivated.Port, evtNodeDeactivated.Raftport)
				err := ptype.UpdatePermissionedNodes(b.Node, b.DataDir, enodeId, ptype.NodeDelete, b.IsRaft)
				if err != nil {
					log.Error("error updating permissioned-nodes.json", "err", err)
				}
				types.NodeInfoMap.UpsertNode(evtNodeDeactivated.OrgId, enodeId, types.NodeDeactivated)

			case evtNodeActivated := <-chNodeActivated:
				enodeId := types.GetNodeUrl(evtNodeActivated.EnodeId, evtNodeActivated.Ip[:], evtNodeActivated.Port, evtNodeActivated.Raftport)
				err := ptype.UpdatePermissionedNodes(b.Node, b.DataDir, enodeId, ptype.NodeAdd, b.IsRaft)
				if err != nil {
					log.Error("error updating permissioned-nodes.json", "err", err)
				}
				types.NodeInfoMap.UpsertNode(evtNodeActivated.OrgId, enodeId, types.NodeApproved)

			case evtNodeBlacklisted := <-chNodeBlacklisted:
				enodeId := types.GetNodeUrl(evtNodeBlacklisted.EnodeId, evtNodeBlacklisted.Ip[:], evtNodeBlacklisted.Port, evtNodeBlacklisted.Raftport)
				types.NodeInfoMap.UpsertNode(evtNodeBlacklisted.OrgId, enodeId, types.NodeBlackListed)
				err := ptype.UpdateDisallowedNodes(b.DataDir, enodeId, ptype.NodeAdd)
				log.Error("error updating disallowed-nodes.json", "err", err)
				err = ptype.UpdatePermissionedNodes(b.Node, b.DataDir, enodeId, ptype.NodeDelete, b.IsRaft)
				if err != nil {
					log.Error("error updating permissioned-nodes.json", "err", err)
				}

			case evtNodeRecoveryInit := <-chNodeRecoveryInit:
				enodeId := types.GetNodeUrl(evtNodeRecoveryInit.EnodeId, evtNodeRecoveryInit.Ip[:], evtNodeRecoveryInit.Port, evtNodeRecoveryInit.Raftport)
				types.NodeInfoMap.UpsertNode(evtNodeRecoveryInit.OrgId, enodeId, types.NodeRecoveryInitiated)

			case evtNodeRecoveryDone := <-chNodeRecoveryDone:
				enodeId := types.GetNodeUrl(evtNodeRecoveryDone.EnodeId, evtNodeRecoveryDone.Ip[:], evtNodeRecoveryDone.Port, evtNodeRecoveryDone.Raftport)
				types.NodeInfoMap.UpsertNode(evtNodeRecoveryDone.OrgId, enodeId, types.NodeApproved)
				err := ptype.UpdateDisallowedNodes(b.DataDir, enodeId, ptype.NodeDelete)
				log.Error("error updating disallowed-nodes.json", "err", err)
				err = ptype.UpdatePermissionedNodes(b.Node, b.DataDir, enodeId, ptype.NodeAdd, b.IsRaft)
				if err != nil {
					log.Error("error updating permissioned-nodes.json", "err", err)
				}

			case <-stopChan:
				log.Info("quit Node contract watch")
				return
			}
		}
	}()
	return nil
}
