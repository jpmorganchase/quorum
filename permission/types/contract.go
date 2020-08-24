package types

import (
	"math/big"
	"reflect"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
)

type ContractService interface {
	RemoveRole(_roleId string, _orgId string) (*types.Transaction, error)
	AddNewRole(_roleId string, _orgId string, _access *big.Int, _voter bool, _admin bool) (*types.Transaction, error)
	ConnectionAllowedImpl(_enodeId string, _ip string, _port uint16, _raftport uint16) (bool, error)
	TransactionAllowed(_srcaccount common.Address, _tgtaccount common.Address) (bool, error)
	AssignAccountRole(_account common.Address, _orgId string, _roleId string) (*types.Transaction, error)
	UpdateAccountStatus(_orgId string, _account common.Address, _action *big.Int) (*types.Transaction, error)
	ApproveBlacklistedNodeRecovery(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _url string) (*types.Transaction, error)
	StartBlacklistedNodeRecovery(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _url string) (*types.Transaction, error)
	StartBlacklistedAccountRecovery(_orgId string, _account common.Address) (*types.Transaction, error)
	ApproveBlacklistedAccountRecovery(_orgId string, _account common.Address) (*types.Transaction, error)
	GetPendingOp(_orgId string) (string, string, common.Address, *big.Int, error)
	ApproveAdminRole(_orgId string, _account common.Address) (*types.Transaction, error)
	AssignAdminRole(_orgId string, _account common.Address, _roleId string) (*types.Transaction, error)
	AddNode(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _url string) (*types.Transaction, error)
	UpdateNodeStatus(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _url string, _action *big.Int) (*types.Transaction, error)
	ApproveOrgStatus(_orgId string, _action *big.Int) (*types.Transaction, error)
	UpdateOrgStatus(_orgId string, _action *big.Int) (*types.Transaction, error)
	ApproveOrg(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _account common.Address, _url string) (*types.Transaction, error)
	AddSubOrg(_pOrgId string, _orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _url string) (*types.Transaction, error)
	AddOrg(_orgId string, _enodeId string, _ip string, _port uint16, _raftport uint16, _account common.Address, _url string) (*types.Transaction, error)
	GetAccountDetailsFromIndex(_aIndex *big.Int) (common.Address, string, string, *big.Int, bool, error)
	GetNumberOfAccounts() (*big.Int, error)
	GetRoleDetailsFromIndex(_rIndex *big.Int) (struct {
		RoleId     string
		OrgId      string
		AccessType *big.Int
		Voter      bool
		Admin      bool
		Active     bool
	}, error)
	GetNumberOfRoles() (*big.Int, error)
	GetNumberOfOrgs() (*big.Int, error)
	UpdateNetworkBootStatus() (*types.Transaction, error)
	AddAdminAccount(_acct common.Address) (*types.Transaction, error)
	AddAdminNode(_enodeId string, _ip string, _port uint16, _raftport uint16) (*types.Transaction, error)
	SetPolicy(_nwAdminOrg string, _nwAdminRole string, _oAdminRole string) (*types.Transaction, error)
	Init(_breadth *big.Int, _depth *big.Int) (*types.Transaction, error)
	GetAccountDetails(_account common.Address) (common.Address, string, string, *big.Int, bool, error)
	GetNodeDetailsFromIndex(_nodeIndex *big.Int) (string, string, *big.Int, error)
	GetNumberOfNodes() (*big.Int, error)
	GetNodeDetails(enodeId string) (string, string, *big.Int, error)
	GetRoleDetails(_roleId string, _orgId string) (struct {
		RoleId     string
		OrgId      string
		AccessType *big.Int
		Voter      bool
		Admin      bool
		Active     bool
	}, error)
	GetSubOrgIndexes(_orgId string) ([]*big.Int, error)
	GetOrgInfo(_orgIndex *big.Int) (string, string, string, *big.Int, *big.Int, error)
	GetNetworkBootStatus() (bool, error)
	GetOrgDetails(_orgId string) (string, string, string, *big.Int, *big.Int, error)
	AfterStart() error
}

func BindContract(contractInstance interface{}, bindFunc func() (interface{}, error)) error {
	element := reflect.ValueOf(contractInstance).Elem()
	instance, err := bindFunc()
	if err != nil {
		return err
	}
	element.Set(reflect.ValueOf(instance))
	return nil
}
