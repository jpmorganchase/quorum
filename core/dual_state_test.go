package core

import (
	"math/big"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/params"
)

var dualStateTestHeader = types.Header{
	Number:     new(big.Int),
	Time:       43,
	Difficulty: new(big.Int).SetUint64(1000488),
	GasLimit:   4700000,
}

//[1] PUSH1 0x01 (out size)
//[3] PUSH1 0x00 (out offset)
//[5] PUSH1 0x00 (in size)
//[7] PUSH1 0x00 (in offset)
//[9] PUSH1 0x00 (value)
//[30] PUSH20 0x0200000000000000000000000000000000000000 (to)
//[34] PUSH3 0x0186a0 (gas)
//[35] CALL
//[37] PUSH1 0x00
//[38] MLOAD
//[40] PUSH1 0x00
//[41] SSTORE
//[42] STOP

func TestDualStatePrivateToPublicCall(t *testing.T) {
	callAddr := common.Address{1}

	db := rawdb.NewMemoryDatabase()
	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(common.Address{2}, common.Hex2Bytes("600a6000526001601ff300"))

	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	privateState.SetCode(callAddr, common.Hex2Bytes("60016000600060006000730200000000000000000000000000000000000000620186a0f160005160005500"))

	author := common.Address{}
	msg := callmsg{
		addr:     author,
		to:       &callAddr,
		value:    big.NewInt(1),
		gas:      1000000,
		gasPrice: new(big.Int),
		data:     nil,
	}

	ctx := NewEVMBlockContext(&dualStateTestHeader, nil, &author)
	txCtx := NewEVMTxContext(msg)
	env := vm.NewEVM(ctx, txCtx, publicState, privateState, &params.ChainConfig{}, vm.Config{})
	env.Call(vm.AccountRef(author), callAddr, msg.data, msg.gas, new(big.Int))

	if value := privateState.GetState(callAddr, common.Hash{}); value != (common.Hash{10}) {
		t.Errorf("expected 10 got %x", value)
	}
}

func TestDualStatePublicToPrivateCall(t *testing.T) {
	callAddr := common.Address{1}

	db := rawdb.NewMemoryDatabase()
	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	privateState.SetCode(common.Address{2}, common.Hex2Bytes("600a6000526001601ff300"))

	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(callAddr, common.Hex2Bytes("60016000600060006000730200000000000000000000000000000000000000620186a0f160005160005500"))

	author := common.Address{}
	msg := callmsg{
		addr:     author,
		to:       &callAddr,
		value:    big.NewInt(1),
		gas:      1000000,
		gasPrice: new(big.Int),
		data:     nil,
	}

	ctx := NewEVMBlockContext(&dualStateTestHeader, nil, &author)
	txCtx := NewEVMTxContext(msg)
	env := vm.NewEVM(ctx, txCtx, publicState, publicState, &params.ChainConfig{}, vm.Config{})
	env.Call(vm.AccountRef(author), callAddr, msg.data, msg.gas, new(big.Int))

	if value := publicState.GetState(callAddr, common.Hash{}); value != (common.Hash{}) {
		t.Errorf("expected 0 got %x", value)
	}
}

func TestDualStateReadOnly(t *testing.T) {
	callAddr := common.Address{1}

	db := rawdb.NewMemoryDatabase()
	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(common.Address{2}, common.Hex2Bytes("600a60005500"))

	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	privateState.SetCode(callAddr, common.Hex2Bytes("60016000600060006000730200000000000000000000000000000000000000620186a0f160005160005500"))

	author := common.Address{}
	msg := callmsg{
		addr:     author,
		to:       &callAddr,
		value:    big.NewInt(1),
		gas:      1000000,
		gasPrice: new(big.Int),
		data:     nil,
	}

	ctx := NewEVMBlockContext(&dualStateTestHeader, nil, &author)
	txCtx := NewEVMTxContext(msg)
	env := vm.NewEVM(ctx, txCtx, publicState, privateState, &params.ChainConfig{}, vm.Config{})
	env.Call(vm.AccountRef(author), callAddr, msg.data, msg.gas, new(big.Int))

	if value := publicState.GetState(common.Address{2}, common.Hash{}); value != (common.Hash{0}) {
		t.Errorf("expected 0 got %x", value)
	}
}

var (
	calleeAddress      = common.Address{2}
	calleeContractCode = "600a6000526001601ff300" // a function that returns 10
	callerAddress      = common.Address{1}
	// a functionn that calls the callee's function at its address and return the same value
	//000000: PUSH1 0x01
	//000002: PUSH1 0x00
	//000004: PUSH1 0x00
	//000006: PUSH1 0x00
	//000008: PUSH20 0x0200000000000000000000000000000000000000
	//000029: PUSH3 0x0186a0
	//000033: STATICCALL
	//000034: PUSH1 0x01
	//000036: PUSH1 0x00
	//000038: RETURN
	//000039: STOP
	callerContractCode = "6001600060006000730200000000000000000000000000000000000000620186a0fa60016000f300"
)

func verifyStaticCall(t *testing.T, privateState *state.StateDB, publicState *state.StateDB, expectedHash common.Hash) {
	author := common.Address{}
	msg := callmsg{
		addr:     author,
		to:       &callerAddress,
		value:    big.NewInt(1),
		gas:      1000000,
		gasPrice: new(big.Int),
		data:     nil,
	}

	ctx := NewEVMBlockContext(&dualStateTestHeader, nil, &author)
	txCtx := NewEVMTxContext(msg)
	env := vm.NewEVM(ctx, txCtx, publicState, privateState, &params.ChainConfig{
		ByzantiumBlock: new(big.Int),
	}, vm.Config{})

	ret, _, err := env.Call(vm.AccountRef(author), callerAddress, msg.data, msg.gas, new(big.Int))

	if err != nil {
		t.Fatalf("Call error: %s", err)
	}
	value := common.Hash{ret[0]}
	if value != expectedHash {
		t.Errorf("expected %x got %x", expectedHash, value)
	}
}

func TestStaticCall_whenPublicToPublic(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(callerAddress, common.Hex2Bytes(callerContractCode))
	publicState.SetCode(calleeAddress, common.Hex2Bytes(calleeContractCode))

	verifyStaticCall(t, publicState, publicState, common.Hash{10})
}

func TestStaticCall_whenPublicToPrivateInTheParty(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	privateState.SetCode(calleeAddress, common.Hex2Bytes(calleeContractCode))

	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(callerAddress, common.Hex2Bytes(callerContractCode))

	verifyStaticCall(t, privateState, publicState, common.Hash{10})
}

func TestStaticCall_whenPublicToPrivateNotInTheParty(t *testing.T) {

	db := rawdb.NewMemoryDatabase()

	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)

	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(callerAddress, common.Hex2Bytes(callerContractCode))

	verifyStaticCall(t, privateState, publicState, common.Hash{0})
}

func TestStaticCall_whenPrivateToPublic(t *testing.T) {
	db := rawdb.NewMemoryDatabase()

	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	privateState.SetCode(callerAddress, common.Hex2Bytes(callerContractCode))

	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState.SetCode(calleeAddress, common.Hex2Bytes(calleeContractCode))

	verifyStaticCall(t, privateState, publicState, common.Hash{10})
}
