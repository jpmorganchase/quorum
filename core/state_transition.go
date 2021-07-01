// Copyright 2014 The go-ethereum Authors
// This file is part of the go-ethereum library.
//
// The go-ethereum library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The go-ethereum library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the go-ethereum library. If not, see <http://www.gnu.org/licenses/>.

package core

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/multitenancy"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/private"
)

/*
The State Transitioning Model

A state transition is a change made when a transaction is applied to the current world state
The state transitioning model does all the necessary work to work out a valid new state root.

1) Nonce handling
2) Pre pay gas
3) Create a new state object if the recipient is \0*32
4) Value transfer
== If contract creation ==
  4a) Attempt to run transaction data
  4b) If valid, use result as code for the new state object
== end ==
5) Run Script section
6) Derive new state root
*/
type StateTransition struct {
	gp         *GasPool
	msg        Message
	gas        uint64
	gasPrice   *big.Int
	initialGas uint64
	value      *big.Int
	data       []byte
	state      vm.StateDB
	evm        *vm.EVM
}

// Message represents a message sent to a contract.
type Message interface {
	From() common.Address
	To() *common.Address

	GasPrice() *big.Int
	Gas() uint64
	Value() *big.Int

	Nonce() uint64
	CheckNonce() bool
	Data() []byte
}

// ExecutionResult includes all output after executing given evm
// message no matter the execution itself is successful or not.
type ExecutionResult struct {
	UsedGas    uint64 // Total used gas but include the refunded gas
	Err        error  // Any error encountered during the execution(listed in core/vm/errors.go)
	ReturnData []byte // Returned data from evm(function result or data supplied with revert opcode)
}

// Unwrap returns the internal evm error which allows us for further
// analysis outside.
func (result *ExecutionResult) Unwrap() error {
	return result.Err
}

// Failed returns the indicator whether the execution is successful or not
func (result *ExecutionResult) Failed() bool { return result.Err != nil }

// Return is a helper function to help caller distinguish between revert reason
// and function return. Return returns the data after execution if no error occurs.
func (result *ExecutionResult) Return() []byte {
	if result.Err != nil {
		return nil
	}
	return common.CopyBytes(result.ReturnData)
}

// Revert returns the concrete revert reason if the execution is aborted by `REVERT`
// opcode. Note the reason can be nil if no data supplied with revert opcode.
func (result *ExecutionResult) Revert() []byte {
	if result.Err != vm.ErrExecutionReverted {
		return nil
	}
	return common.CopyBytes(result.ReturnData)
}

// PrivateMessage implements a private message
type PrivateMessage interface {
	Message
	IsPrivate() bool
}

// IntrinsicGas computes the 'intrinsic gas' for a message with the given data.
func IntrinsicGas(data []byte, contractCreation, isHomestead bool, isEIP2028 bool) (uint64, error) {
	// Set the starting gas for the raw transaction
	var gas uint64
	if contractCreation && isHomestead {
		gas = params.TxGasContractCreation
	} else {
		gas = params.TxGas
	}
	// Bump the required gas by the amount of transactional data
	if len(data) > 0 {
		// Zero and non-zero bytes are priced differently
		var nz uint64
		for _, byt := range data {
			if byt != 0 {
				nz++
			}
		}
		// Make sure we don't exceed uint64 for all data combinations
		nonZeroGas := params.TxDataNonZeroGasFrontier
		if isEIP2028 {
			nonZeroGas = params.TxDataNonZeroGasEIP2028
		}
		if (math.MaxUint64-gas)/nonZeroGas < nz {
			return 0, ErrGasUintOverflow
		}
		gas += nz * nonZeroGas

		z := uint64(len(data)) - nz
		if (math.MaxUint64-gas)/params.TxDataZeroGas < z {
			return 0, ErrGasUintOverflow
		}
		gas += z * params.TxDataZeroGas
	}
	return gas, nil
}

// NewStateTransition initialises and returns a new state transition object.
func NewStateTransition(evm *vm.EVM, msg Message, gp *GasPool) *StateTransition {
	return &StateTransition{
		gp:       gp,
		evm:      evm,
		msg:      msg,
		gasPrice: msg.GasPrice(),
		value:    msg.Value(),
		data:     msg.Data(),
		state:    evm.PublicState(),
	}
}

// ApplyMessage computes the new state by applying the given message
// against the old state within the environment.
//
// ApplyMessage returns the bytes returned by any EVM execution (if it took place),
// the gas used (which includes gas refunds) and an error if it failed. An error always
// indicates a core error meaning that the message would always fail for that particular
// state and would never be accepted within a block.

func ApplyMessage(evm *vm.EVM, msg Message, gp *GasPool) (*ExecutionResult, error) {
	return NewStateTransition(evm, msg, gp).TransitionDb()
}

// to returns the recipient of the message.
func (st *StateTransition) to() common.Address {
	if st.msg == nil || st.msg.To() == nil /* contract creation */ {
		return common.Address{}
	}
	return *st.msg.To()
}

func (st *StateTransition) buyGas() error {
	mgval := new(big.Int).Mul(new(big.Int).SetUint64(st.msg.Gas()), st.gasPrice)
	if have, want := st.state.GetBalance(st.msg.From()), mgval; have.Cmp(want) < 0 {
		return fmt.Errorf("%w: address %v have %v want %v", ErrInsufficientFunds, st.msg.From().Hex(), have, want)
	}
	if err := st.gp.SubGas(st.msg.Gas()); err != nil {
		return err
	}
	st.gas += st.msg.Gas()

	st.initialGas = st.msg.Gas()
	st.state.SubBalance(st.msg.From(), mgval)
	return nil
}

func (st *StateTransition) preCheck() error {
	// Make sure this transaction's nonce is correct.
	if st.msg.CheckNonce() {
		stNonce := st.state.GetNonce(st.msg.From())
		if msgNonce := st.msg.Nonce(); stNonce < msgNonce {
			return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooHigh,
				st.msg.From().Hex(), msgNonce, stNonce)
		} else if stNonce > msgNonce {
			return fmt.Errorf("%w: address %v, tx: %d state: %d", ErrNonceTooLow,
				st.msg.From().Hex(), msgNonce, stNonce)
		}
	}
	return st.buyGas()
}

// TransitionDb will transition the state by applying the current message and
// returning the evm execution result with following fields.
//
// - used gas:
//      total gas used (including gas being refunded)
// - returndata:
//      the returned data from evm
// - concrete execution error:
//      various **EVM** error which aborts the execution,
//      e.g. ErrOutOfGas, ErrExecutionReverted
//
// However if any consensus issue encountered, return the error directly with
// nil evm execution result.
//
// Quorum:
// 1. Intrinsic gas is calculated based on the encrypted payload hash
//    and NOT the actual private payload
// 2. For private transactions, we only deduct intrinsic gas from the gas pool
//    regardless the current node is party to the transaction or not
// 3. With multitenancy support, we enforce the party set in the contract index must contain all
//    parties from the transaction. This is to detect unauthorized access from a legit proxy contract
//    to an unauthorized contract.
func (st *StateTransition) TransitionDb() (*ExecutionResult, error) {
	// First check this message satisfies all consensus rules before
	// applying the message. The rules include these clauses
	//
	// 1. the nonce of the message caller is correct
	// 2. caller has enough balance to cover transaction fee(gaslimit * gasprice)
	// 3. the amount of gas required is available in the block
	// 4. the purchased gas is enough to cover intrinsic usage
	// 5. there is no overflow when calculating intrinsic gas
	// 6. caller has enough balance to cover asset transfer for **topmost** call

	// Check clauses 1-3, buy gas if everything is correct
	var err error
	if err = st.preCheck(); err != nil {
		return nil, err
	}
	msg := st.msg
	sender := vm.AccountRef(msg.From())
	homestead := st.evm.ChainConfig().IsHomestead(st.evm.Context.BlockNumber)
	istanbul := st.evm.ChainConfig().IsIstanbul(st.evm.Context.BlockNumber)
	contractCreation := msg.To() == nil
	isQuorum := st.evm.ChainConfig().IsQuorum
	snapshot := st.evm.StateDB.Snapshot()

	var data []byte
	isPrivate := false
	publicState := st.state
	pmh := newPMH(st)
	if msg, ok := msg.(PrivateMessage); ok && isQuorum && msg.IsPrivate() {
		isPrivate = true
		pmh.snapshot = snapshot
		pmh.eph = common.BytesToEncryptedPayloadHash(st.data)
		_, _, data, pmh.receivedPrivacyMetadata, err = private.P.Receive(pmh.eph)
		// Increment the public account nonce if:
		// 1. Tx is private and *not* a participant of the group and either call or create
		// 2. Tx is private we are part of the group and is a call
		if err != nil || !contractCreation {
			publicState.SetNonce(sender.Address(), publicState.GetNonce(sender.Address())+1)
		}
		if err != nil {
			return &ExecutionResult{
				UsedGas:    0,
				Err:        nil,
				ReturnData: nil,
			}, nil
		}

		pmh.hasPrivatePayload = data != nil

		vmErr, consensusErr := pmh.prepare()
		if consensusErr != nil || vmErr != nil {
			return &ExecutionResult{
				UsedGas:    0,
				Err:        vmErr,
				ReturnData: nil,
			}, consensusErr
		}
	} else {
		data = st.data
	}

	// Pay intrinsic gas. For a private contract this is done using the public hash passed in,
	// not the private data retrieved above. This is because we need any (participant) validator
	// node to get the same result as a (non-participant) minter node, to avoid out-of-gas issues.
	// Check clauses 4-5, subtract intrinsic gas if everything is correct
	gas, err := IntrinsicGas(st.data, contractCreation, homestead, istanbul)
	if err != nil {
		return nil, err
	}
	if st.gas < gas {
		return nil, fmt.Errorf("%w: have %d, want %d", ErrIntrinsicGas, st.gas, gas)
	}
	st.gas -= gas

	// Check clause 6
	if msg.Value().Sign() > 0 && !st.evm.Context.CanTransfer(st.state, msg.From(), msg.Value()) {
		return nil, fmt.Errorf("%w: address %v", ErrInsufficientFundsForTransfer, msg.From().Hex())
	}
	var (
		leftoverGas uint64
		evm         = st.evm
		ret         []byte
		// vm errors do not effect consensus and are therefor
		// not assigned to err, except for insufficient balance
		// error.
		vmerr error
	)
	if contractCreation {
		ret, _, leftoverGas, vmerr = evm.Create(sender, data, st.gas, st.value)
	} else {
		// Increment the account nonce only if the transaction isn't private.
		// If the transaction is private it has already been incremented on
		// the public state.
		if !isPrivate {
			publicState.SetNonce(msg.From(), publicState.GetNonce(sender.Address())+1)
		}
		var to common.Address
		if isQuorum {
			to = *st.msg.To()
		} else {
			to = st.to()
		}
		//if input is empty for the smart contract call, return
		if len(data) == 0 && isPrivate {
			st.refundGas()
			st.state.AddBalance(st.evm.Context.Coinbase, new(big.Int).Mul(new(big.Int).SetUint64(st.gasUsed()), st.gasPrice))
			return &ExecutionResult{
				UsedGas:    0,
				Err:        nil,
				ReturnData: nil,
			}, nil
		}

		ret, leftoverGas, vmerr = evm.Call(sender, to, data, st.gas, st.value)
	}
	if vmerr != nil {
		log.Info("VM returned with error", "err", vmerr)
		// The only possible consensus-error would be if there wasn't
		// sufficient balance to make the transfer happen. The first
		// balance transfer may never fail.
		if vmerr == vm.ErrInsufficientBalance {
			return nil, vmerr
		}
		if errors.Is(vmerr, multitenancy.ErrNotAuthorized) {
			return nil, vmerr
		}
	}

	// Quorum - Privacy Enhancements
	// perform privacy enhancements checks
	if pmh.mustVerify() {
		var exitEarly bool
		exitEarly, err = pmh.verify(vmerr)
		if exitEarly {
			return &ExecutionResult{
				UsedGas:    0,
				Err:        ErrPrivateContractInteractionVerificationFailed,
				ReturnData: nil,
			}, err
		}
	}
	// End Quorum - Privacy Enhancements

	// Pay gas used during contract creation or execution (st.gas tracks remaining gas)
	// However, if private contract then we don't want to do this else we can get
	// a mismatch between a (non-participant) minter and (participant) validator,
	// which can cause a 'BAD BLOCK' crash.
	if !isPrivate {
		st.gas = leftoverGas
	}
	// End Quorum

	st.refundGas()
	st.state.AddBalance(st.evm.Context.Coinbase, new(big.Int).Mul(new(big.Int).SetUint64(st.gasUsed()), st.gasPrice))

	if isPrivate {
		return &ExecutionResult{
			UsedGas:    0,
			Err:        vmerr,
			ReturnData: ret,
		}, err
	}
	// End Quorum

	return &ExecutionResult{
		UsedGas:    st.gasUsed(),
		Err:        vmerr,
		ReturnData: ret,
	}, nil
}

func (st *StateTransition) refundGas() {
	// Apply refund counter, capped to half of the used gas.
	refund := st.gasUsed() / 2
	if refund > st.state.GetRefund() {
		refund = st.state.GetRefund()
	}
	st.gas += refund

	// Return ETH for remaining gas, exchanged at the original rate.
	remaining := new(big.Int).Mul(new(big.Int).SetUint64(st.gas), st.gasPrice)
	st.state.AddBalance(st.msg.From(), remaining)

	// Also return remaining gas to the block gas counter so it is
	// available for the next transaction.
	st.gp.AddGas(st.gas)
}

// gasUsed returns the amount of gas used up by the state transition.
func (st *StateTransition) gasUsed() uint64 {
	return st.initialGas - st.gas
}

// Quorum - Privacy Enhancements - implement the pmcStateTransitionAPI interface
func (st *StateTransition) SetTxPrivacyMetadata(pm *types.PrivacyMetadata) {
	st.evm.SetTxPrivacyMetadata(pm)
}
func (st *StateTransition) IsPrivacyEnhancementsEnabled() bool {
	return st.evm.ChainConfig().IsPrivacyEnhancementsEnabled(st.evm.Context.BlockNumber)
}
func (st *StateTransition) RevertToSnapshot(snapshot int) {
	st.evm.StateDB.RevertToSnapshot(snapshot)
}
func (st *StateTransition) GetStatePrivacyMetadata(addr common.Address) (*state.PrivacyMetadata, error) {
	return st.evm.StateDB.GetPrivacyMetadata(addr)
}
func (st *StateTransition) CalculateMerkleRoot() (common.Hash, error) {
	return st.evm.CalculateMerkleRoot()
}
func (st *StateTransition) AffectedContracts() []common.Address {
	return st.evm.AffectedContracts()
}

// End Quorum - Privacy Enhancements
