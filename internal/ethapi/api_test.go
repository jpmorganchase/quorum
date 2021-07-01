package ethapi

import (
	"context"
	"math/big"
	"os"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common/math"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/mps"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/multitenancy"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/private"
	"github.com/ethereum/go-ethereum/private/engine"
	"github.com/ethereum/go-ethereum/private/engine/notinuse"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/ethereum/go-ethereum/trie"
	"github.com/golang/mock/gomock"
	"github.com/jpmorganchase/quorum-security-plugin-sdk-go/proto"
	"github.com/stretchr/testify/assert"
)

var (
	arbitraryCtx         = context.Background()
	arbitraryPrivateFrom = "arbitrary private from"
	privateTxArgs        = &PrivateTxArgs{
		PrivateFrom: arbitraryPrivateFrom,
		PrivateFor:  []string{"arbitrary party 1", "arbitrary party 2"},
	}
	arbitraryFrom         = common.BytesToAddress([]byte("arbitrary address"))
	arbitraryTo           = common.BytesToAddress([]byte("arbitrary address to"))
	arbitraryGas          = uint64(200000)
	arbitraryZeroGasPrice = big.NewInt(0)
	arbitraryZeroValue    = big.NewInt(0)
	arbitraryEmptyData    = new([]byte)
	callTxArgs            = CallArgs{
		From:     &arbitraryFrom,
		To:       &arbitraryTo,
		Gas:      (*hexutil.Uint64)(&arbitraryGas),
		GasPrice: (*hexutil.Big)(arbitraryZeroGasPrice),
		Value:    (*hexutil.Big)(arbitraryZeroValue),
		Data:     (*hexutil.Bytes)(arbitraryEmptyData),
	}

	arbitrarySimpleStorageContractEncryptedPayloadHash = common.BytesToEncryptedPayloadHash([]byte("arbitrary payload hash"))

	simpleStorageContractCreationTx = types.NewContractCreation(
		0,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		hexutil.MustDecode("0x6060604052341561000f57600080fd5b604051602080610149833981016040528080519060200190919050505b806000819055505b505b610104806100456000396000f30060606040526000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff1680632a1afcd914605157806360fe47b11460775780636d4ce63c146097575b600080fd5b3415605b57600080fd5b606160bd565b6040518082815260200191505060405180910390f35b3415608157600080fd5b6095600480803590602001909190505060c3565b005b341560a157600080fd5b60a760ce565b6040518082815260200191505060405180910390f35b60005481565b806000819055505b50565b6000805490505b905600a165627a7a72305820d5851baab720bba574474de3d09dbeaabc674a15f4dd93b974908476542c23f00029"))

	rawSimpleStorageContractCreationTx = types.NewContractCreation(
		0,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		arbitrarySimpleStorageContractEncryptedPayloadHash.Bytes())

	arbitrarySimpleStorageContractAddress                common.Address
	arbitraryStandardPrivateSimpleStorageContractAddress common.Address

	simpleStorageContractMessageCallTx                   *types.Transaction
	standardPrivateSimpleStorageContractMessageCallTx    *types.Transaction
	rawStandardPrivateSimpleStorageContractMessageCallTx *types.Transaction

	arbitraryCurrentBlockNumber = big.NewInt(1)

	publicStateDB  *state.StateDB
	privateStateDB *state.StateDB
)

func TestMain(m *testing.M) {
	setup()
	retCode := m.Run()
	teardown()
	os.Exit(retCode)
}

func setup() {
	log.Root().SetHandler(log.StreamHandler(os.Stdout, log.TerminalFormat(true)))
	var err error

	memdb := rawdb.NewMemoryDatabase()
	db := state.NewDatabase(memdb)

	publicStateDB, err = state.New(common.Hash{}, db, nil)
	if err != nil {
		panic(err)
	}
	privateStateDB, err = state.New(common.Hash{}, db, nil)
	if err != nil {
		panic(err)
	}

	private.P = &StubPrivateTransactionManager{}

	key, _ := crypto.GenerateKey()
	from := crypto.PubkeyToAddress(key.PublicKey)

	arbitrarySimpleStorageContractAddress = crypto.CreateAddress(from, 0)

	simpleStorageContractMessageCallTx = types.NewTransaction(
		0,
		arbitrarySimpleStorageContractAddress,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		hexutil.MustDecode("0x60fe47b1000000000000000000000000000000000000000000000000000000000000000d"))

	arbitraryStandardPrivateSimpleStorageContractAddress = crypto.CreateAddress(from, 1)

	standardPrivateSimpleStorageContractMessageCallTx = types.NewTransaction(
		0,
		arbitraryStandardPrivateSimpleStorageContractAddress,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		hexutil.MustDecode("0x60fe47b1000000000000000000000000000000000000000000000000000000000000000e"))

	rawStandardPrivateSimpleStorageContractMessageCallTx = types.NewTransaction(
		0,
		arbitraryStandardPrivateSimpleStorageContractAddress,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		arbitrarySimpleStorageContractEncryptedPayloadHash.Bytes())

}

func teardown() {
	log.Root().SetHandler(log.DiscardHandler())
}

func TestDoEstimateGas_whenNoValueTx_Pre_Istanbul(t *testing.T) {
	assert := assert.New(t)

	estimation, err := DoEstimateGas(arbitraryCtx, &StubBackend{CurrentHeadNumber: big.NewInt(10)}, callTxArgs, rpc.BlockNumberOrHashWithNumber(10), math.MaxInt64)

	assert.NoError(err, "gas estimation")
	assert.Equal(hexutil.Uint64(25352), estimation, "estimation for a public or private tx")
}

func TestDoEstimateGas_whenNoValueTx_Istanbul(t *testing.T) {
	assert := assert.New(t)

	estimation, err := DoEstimateGas(arbitraryCtx, &StubBackend{IstanbulBlock: big.NewInt(0), CurrentHeadNumber: big.NewInt(10)}, callTxArgs, rpc.BlockNumberOrHashWithNumber(10), math.MaxInt64)

	assert.NoError(err, "gas estimation")
	assert.Equal(hexutil.Uint64(22024), estimation, "estimation for a public or private tx")
}

func TestSimulateExecution_whenStandardPrivateCreation(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractCreationTx, privateTxArgs)

	assert.NoError(err, "simulate execution")
	assert.Empty(affectedCACreationTxHashes, "creation tx should not have any affected contract creation tx hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
}

func TestSimulateExecution_whenPartyProtectionCreation(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractCreationTx, privateTxArgs)

	assert.NoError(err, "simulation execution")
	assert.Empty(affectedCACreationTxHashes, "creation tx should not have any affected contract creation tx hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
}

func TestSimulateExecution_whenCreationWithStateValidation(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStateValidation

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractCreationTx, privateTxArgs)

	assert.NoError(err, "simulate execution")
	assert.Empty(affectedCACreationTxHashes, "creation tx should not have any affected contract creation tx hashes")
	assert.NotEqual(common.Hash{}, merkleRoot, "private state validation")
}

func TestSimulateExecution_whenStandardPrivateMessageCall(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	privateStateDB.SetCode(arbitraryStandardPrivateSimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000002"))
	privateStateDB.SetState(arbitraryStandardPrivateSimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs)

	log.Debug("state", "state", privateStateDB.GetState(arbitraryStandardPrivateSimpleStorageContractAddress, common.Hash{0}))

	assert.NoError(err, "simulate execution")
	assert.Empty(affectedCACreationTxHashes, "standard private contract should not have any affected contract creation tx hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
}

func TestSimulateExecution_StandardPrivateMessageCallSucceedsWheContractNotAvailableLocally(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs)

	log.Debug("state", "state", privateStateDB.GetState(arbitraryStandardPrivateSimpleStorageContractAddress, common.Hash{0}))

	assert.NoError(err, "simulate execution")
	assert.Empty(affectedCACreationTxHashes, "standard private contract should not have any affected contract creation tx hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
}

func TestSimulateExecution_whenPartyProtectionMessageCall(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    privateTxArgs.PrivacyFlag,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	expectedCACreationTxHashes := []common.EncryptedPayloadHash{arbitrarySimpleStorageContractEncryptedPayloadHash}

	log.Debug("state", "state", privateStateDB.GetState(arbitrarySimpleStorageContractAddress, common.Hash{0}))

	assert.NoError(err, "simulate execution")
	assert.NotEmpty(affectedCACreationTxHashes, "affected contract accounts' creation transacton hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
	assert.True(len(affectedCACreationTxHashes) == len(expectedCACreationTxHashes))
}

func TestSimulateExecution_whenPartyProtectionMessageCallAndPrivacyEnhancementsDisabled(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	params.QuorumTestChainConfig.PrivacyEnhancementsBlock = nil
	defer func() { params.QuorumTestChainConfig.PrivacyEnhancementsBlock = big.NewInt(0) }()

	stbBackend := &StubBackend{}
	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, stbBackend, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	// the simulation returns early without executing the transaction
	assert.False(stbBackend.getEVMCalled, "simulation is ended early - before getEVM is called")
	assert.NoError(err, "simulate execution")
	assert.Empty(affectedCACreationTxHashes, "affected contract accounts' creation transacton hashes")
	assert.Equal(common.Hash{}, merkleRoot, "no private state validation")
}

func TestSimulateExecution_whenStateValidationMessageCall(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStateValidation

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    privateTxArgs.PrivacyFlag,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	affectedCACreationTxHashes, merkleRoot, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	expectedCACreationTxHashes := []common.EncryptedPayloadHash{arbitrarySimpleStorageContractEncryptedPayloadHash}

	log.Debug("state", "state", privateStateDB.GetState(arbitrarySimpleStorageContractAddress, common.Hash{0}))

	assert.NoError(err, "simulate execution")
	assert.NotEmpty(affectedCACreationTxHashes, "affected contract accounts' creation transacton hashes")
	assert.NotEqual(common.Hash{}, merkleRoot, "private state validation")
	assert.True(len(affectedCACreationTxHashes) == len(expectedCACreationTxHashes))
}

//mix and match flags
func TestSimulateExecution_PrivacyFlagPartyProtectionCallingStandardPrivateContract_Error(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	privateStateDB.SetCode(arbitraryStandardPrivateSimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000002"))
	privateStateDB.SetState(arbitraryStandardPrivateSimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	_, _, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs)

	log.Debug("state", "state", privateStateDB.GetState(arbitraryStandardPrivateSimpleStorageContractAddress, common.Hash{0}))

	assert.Error(err, "simulate execution")
}

func TestSimulateExecution_StandardPrivateFlagCallingPartyProtectionContract_Error(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    engine.PrivacyFlagPartyProtection,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	_, _, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	assert.Error(err, "simulate execution")
}

func TestSimulateExecution_StandardPrivateFlagCallingStateValidationContract_Error(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    engine.PrivacyFlagStateValidation,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	_, _, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	log.Debug("state", "state", privateStateDB.GetState(arbitrarySimpleStorageContractAddress, common.Hash{0}))

	assert.Error(err, "simulate execution")
}

func TestSimulateExecution_PartyProtectionFlagCallingStateValidationContract_Error(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    engine.PrivacyFlagStateValidation,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	_, _, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	log.Debug("state", "state", privateStateDB.GetState(arbitrarySimpleStorageContractAddress, common.Hash{0}))

	assert.Error(err, "simulate execution")
}

func TestSimulateExecution_StateValidationFlagCallingPartyProtectionContract_Error(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStateValidation

	privateStateDB.SetCode(arbitrarySimpleStorageContractAddress, hexutil.MustDecode("0x608060405234801561001057600080fd5b506040516020806101618339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610109806100586000396000f3fe6080604052600436106049576000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff16806360fe47b114604e5780636d4ce63c146099575b600080fd5b348015605957600080fd5b50608360048036036020811015606e57600080fd5b810190808035906020019092919050505060c1565b6040518082815260200191505060405180910390f35b34801560a457600080fd5b5060ab60d4565b6040518082815260200191505060405180910390f35b6000816000819055506000549050919050565b6000805490509056fea165627a7a723058203624ca2e3479d3fa5a12d97cf3dae0d9a6de3a3b8a53c8605b9cd398d9766b9f00290000000000000000000000000000000000000000000000000000000000000001"))
	privateStateDB.SetPrivacyMetadata(arbitrarySimpleStorageContractAddress, &state.PrivacyMetadata{
		PrivacyFlag:    engine.PrivacyFlagPartyProtection,
		CreationTxHash: arbitrarySimpleStorageContractEncryptedPayloadHash,
	})

	privateStateDB.SetState(arbitrarySimpleStorageContractAddress, common.Hash{0}, common.Hash{100})
	privateStateDB.Commit(true)

	_, _, err := simulateExecutionForPE(arbitraryCtx, &StubBackend{}, arbitraryFrom, simpleStorageContractMessageCallTx, privateTxArgs)

	//expectedCACreationTxHashes := []common.EncryptedPayloadHash{arbitrarySimpleStorageContractEncryptedPayloadHash}

	log.Debug("state", "state", privateStateDB.GetState(arbitrarySimpleStorageContractAddress, common.Hash{0}))

	assert.Error(err, "simulate execution")
}

func TestHandlePrivateTransaction_whenInvalidFlag(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = 4

	_, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, simpleStorageContractCreationTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.Error(err, "invalid privacyFlag")
}

func TestHandlePrivateTransaction_whenPrivateFromDoesNotMatchPrivateState(t *testing.T) {
	assert := assert.New(t)
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockpsm := mps.NewMockPrivateStateManager(mockCtrl)
	mockpsm.EXPECT().ResolveForUserContext(gomock.Any()).Return(mps.NewPrivateStateMetadata("PS1", "PS1", "", mps.Resident, []string{"some address"}), nil).AnyTimes()

	_, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &MPSStubBackend{psmr: mockpsm}, simpleStorageContractCreationTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.Error(err, "The PrivateFrom (arbitrary private from) address does not match the specified private state (PS1) ")
}

func TestHandlePrivateTransaction_whenPrivateFromMatchesPrivateState(t *testing.T) {
	assert := assert.New(t)
	mockCtrl := gomock.NewController(t)
	defer mockCtrl.Finish()

	mockpsm := mps.NewMockPrivateStateManager(mockCtrl)
	mockpsm.EXPECT().ResolveForUserContext(gomock.Any()).Return(mps.NewPrivateStateMetadata("PS1", "PS1", "", mps.Resident, []string{"some address"}), nil).AnyTimes()

	// empty data field means that checkAndHandlePrivateTransaction exits without doing handlePrivateTransaction
	emptyTx := types.NewContractCreation(
		0,
		big.NewInt(0),
		hexutil.MustDecodeUint64("0x47b760"),
		big.NewInt(0),
		nil)

	mpsTxArgs := &PrivateTxArgs{
		PrivateFrom: "some address",
		PrivateFor:  []string{"arbitrary party 1", "arbitrary party 2"},
	}
	_, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &MPSStubBackend{psmr: mockpsm}, emptyTx, mpsTxArgs, arbitraryFrom, NormalTransaction)

	assert.Nil(err)
}

func TestHandlePrivateTransaction_withPartyProtectionTxAndPrivacyEnhancementsIsDisabled(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = 1
	params.QuorumTestChainConfig.PrivacyEnhancementsBlock = nil
	defer func() { params.QuorumTestChainConfig.PrivacyEnhancementsBlock = big.NewInt(0) }()

	_, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, simpleStorageContractCreationTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.Error(err, "PrivacyEnhancements are disabled. Can only accept transactions with PrivacyFlag=0(StandardPrivate).")
}

func TestHandlePrivateTransaction_whenStandardPrivateCreation(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	isPrivate, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, simpleStorageContractCreationTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	if err != nil {
		t.Fatalf("%s", err)
	}

	assert.True(isPrivate, "must be a private transaction")
}

func TestHandlePrivateTransaction_whenStandardPrivateCallingContractThatIsNotAvailableLocally(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	isPrivate, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.NoError(err, "no error expected")

	assert.True(isPrivate, "must be a private transaction")
}

func TestHandlePrivateTransaction_whenPartyProtectionCallingContractThatIsNotAvailableLocally(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	isPrivate, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.Error(err, "handle invalid message call")

	assert.True(isPrivate, "must be a private transaction")
}

func TestHandlePrivateTransaction_whenPartyProtectionCallingStandardPrivate(t *testing.T) {
	assert := assert.New(t)
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagPartyProtection

	isPrivate, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, standardPrivateSimpleStorageContractMessageCallTx, privateTxArgs, arbitraryFrom, NormalTransaction)

	assert.Error(err, "handle invalid message call")

	assert.True(isPrivate, "must be a private transaction")
}

func TestHandlePrivateTransaction_whenRawStandardPrivateCreation(t *testing.T) {
	assert := assert.New(t)
	private.P = &StubPrivateTransactionManager{creation: true}
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	isPrivate, _, err := checkAndHandlePrivateTransaction(arbitraryCtx, &StubBackend{}, rawSimpleStorageContractCreationTx, privateTxArgs, arbitraryFrom, RawTransaction)

	assert.NoError(err, "raw standard private creation succeeded")
	assert.True(isPrivate, "must be a private transaction")
}

func TestHandlePrivateTransaction_whenRawStandardPrivateMessageCall(t *testing.T) {
	assert := assert.New(t)
	private.P = &StubPrivateTransactionManager{creation: false}
	privateTxArgs.PrivacyFlag = engine.PrivacyFlagStandardPrivate

	_, err := handlePrivateTransaction(arbitraryCtx, &StubBackend{}, rawStandardPrivateSimpleStorageContractMessageCallTx, privateTxArgs, arbitraryFrom, RawTransaction)

	assert.NoError(err, "raw standard private msg call succeeded")

}

type StubBackend struct {
	getEVMCalled                    bool
	mockAccountExtraDataStateGetter *vm.MockAccountExtraDataStateGetter

	IstanbulBlock     *big.Int
	CurrentHeadNumber *big.Int
}

func (sb *StubBackend) CurrentHeader() *types.Header {
	return &types.Header{Number: sb.CurrentHeadNumber}
}

func (sb *StubBackend) Engine() consensus.Engine {
	panic("implement me")
}

func (sb *StubBackend) SupportsMultitenancy(rpcCtx context.Context) (*proto.PreAuthenticatedAuthenticationToken, bool) {
	return nil, false
}

func (sb *StubBackend) AccountExtraDataStateGetterByNumber(context.Context, rpc.BlockNumber) (vm.AccountExtraDataStateGetter, error) {
	return sb.mockAccountExtraDataStateGetter, nil
}

func (sb *StubBackend) IsAuthorized(authToken *proto.PreAuthenticatedAuthenticationToken, attributes ...*multitenancy.PrivateStateSecurityAttribute) (bool, error) {
	panic("implement me")
}

func (sb *StubBackend) GetEVM(ctx context.Context, msg core.Message, state vm.MinimalApiState, header *types.Header) (*vm.EVM, func() error, error) {
	sb.getEVMCalled = true
	vmCtx := core.NewEVMBlockContext(&types.Header{
		Coinbase:   arbitraryFrom,
		Number:     arbitraryCurrentBlockNumber,
		Time:       0,
		Difficulty: big.NewInt(0),
		GasLimit:   0,
	}, nil, &arbitraryFrom)
	txCtx := core.NewEVMTxContext(msg)
	vmError := func() error {
		return nil
	}
	config := params.QuorumTestChainConfig
	config.IstanbulBlock = sb.IstanbulBlock
	return vm.NewEVM(vmCtx, txCtx, publicStateDB, privateStateDB, config, vm.Config{}), vmError, nil
}

func (sb *StubBackend) CurrentBlock() *types.Block {
	return types.NewBlock(&types.Header{
		Number: arbitraryCurrentBlockNumber,
	}, nil, nil, nil, new(trie.Trie))
}

func (sb *StubBackend) Downloader() *downloader.Downloader {
	panic("implement me")
}

func (sb *StubBackend) ProtocolVersion() int {
	panic("implement me")
}

func (sb *StubBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	panic("implement me")
}

func (sb *StubBackend) ChainDb() ethdb.Database {
	panic("implement me")
}

func (sb *StubBackend) EventMux() *event.TypeMux {
	panic("implement me")
}

func (sb *StubBackend) AccountManager() *accounts.Manager {
	panic("implement me")
}

func (sb *StubBackend) ExtRPCEnabled() bool {
	panic("implement me")
}

func (sb *StubBackend) CallTimeOut() time.Duration {
	panic("implement me")
}

func (sb *StubBackend) RPCTxFeeCap() float64 {
	panic("implement me")
}

func (sb *StubBackend) RPCGasCap() uint64 {
	panic("implement me")
}

func (sb *StubBackend) SetHead(number uint64) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByNumber(ctx context.Context, blockNr rpc.BlockNumber) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	panic("implement me")
}

func (sb *StubBackend) StateAndHeaderByNumber(ctx context.Context, blockNr rpc.BlockNumber) (vm.MinimalApiState, *types.Header, error) {
	return &StubMinimalApiState{}, nil, nil
}

func (sb *StubBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.MinimalApiState, *types.Header, error) {
	return &StubMinimalApiState{}, nil, nil
}

func (sb *StubBackend) GetReceipts(ctx context.Context, blockHash common.Hash) (types.Receipts, error) {
	panic("implement me")
}

func (sb *StubBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	panic("implement me")
}

func (sb *StubBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	panic("implement me")
}

func (sb *StubBackend) GetPoolTransactions() (types.Transactions, error) {
	panic("implement me")
}

func (sb *StubBackend) GetPoolTransaction(txHash common.Hash) *types.Transaction {
	panic("implement me")
}

func (sb *StubBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	panic("implement me")
}

func (sb *StubBackend) Stats() (pending int, queued int) {
	panic("implement me")
}

func (sb *StubBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	panic("implement me")
}

func (sb *StubBackend) SubscribeNewTxsEvent(chan<- core.NewTxsEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) BloomStatus() (uint64, uint64) {
	panic("implement me")
}

func (sb *StubBackend) GetLogs(ctx context.Context, blockHash common.Hash) ([][]*types.Log, error) {
	panic("implement me")
}

func (sb *StubBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	panic("implement me")
}

func (sb *StubBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) ChainConfig() *params.ChainConfig {
	return params.QuorumTestChainConfig
}

func (sb *StubBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	panic("implement me")
}

func (sb *StubBackend) PSMR() mps.PrivateStateMetadataResolver {
	panic("implement me")
}

type MPSStubBackend struct {
	StubBackend
	psmr mps.PrivateStateMetadataResolver
}

func (msb *MPSStubBackend) ChainConfig() *params.ChainConfig {
	return params.QuorumMPSTestChainConfig
}

func (sb *MPSStubBackend) PSMR() mps.PrivateStateMetadataResolver {
	return sb.psmr
}

type StubMinimalApiState struct {
}

func (StubMinimalApiState) GetBalance(addr common.Address) *big.Int {
	panic("implement me")
}

func (StubMinimalApiState) SetBalance(addr common.Address, balance *big.Int) {
	panic("implement me")
}

func (StubMinimalApiState) GetCode(addr common.Address) []byte {
	return nil
}

func (StubMinimalApiState) GetState(a common.Address, b common.Hash) common.Hash {
	panic("implement me")
}

func (StubMinimalApiState) GetNonce(addr common.Address) uint64 {
	panic("implement me")
}

func (StubMinimalApiState) SetNonce(addr common.Address, nonce uint64) {
	panic("implement me")
}

func (StubMinimalApiState) SetCode(common.Address, []byte) {
	panic("implement me")
}

func (StubMinimalApiState) GetPrivacyMetadata(addr common.Address) (*state.PrivacyMetadata, error) {
	panic("implement me")
}

func (StubMinimalApiState) GetManagedParties(addr common.Address) ([]string, error) {
	panic("implement me")
}

func (StubMinimalApiState) GetRLPEncodedStateObject(addr common.Address) ([]byte, error) {
	panic("implement me")
}

func (StubMinimalApiState) GetProof(common.Address) ([][]byte, error) {
	panic("implement me")
}

func (StubMinimalApiState) GetStorageProof(common.Address, common.Hash) ([][]byte, error) {
	panic("implement me")
}

func (StubMinimalApiState) StorageTrie(addr common.Address) state.Trie {
	panic("implement me")
}

func (StubMinimalApiState) Error() error {
	panic("implement me")
}

func (StubMinimalApiState) GetCodeHash(common.Address) common.Hash {
	panic("implement me")
}

func (StubMinimalApiState) SetState(common.Address, common.Hash, common.Hash) {
	panic("implement me")
}

func (StubMinimalApiState) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) {
	panic("implement me")
}

type StubPrivateTransactionManager struct {
	notinuse.PrivateTransactionManager
	creation bool
}

func (sptm *StubPrivateTransactionManager) Send(data []byte, from string, to []string, extra *engine.ExtraMetadata) (string, []string, common.EncryptedPayloadHash, error) {
	return "", nil, arbitrarySimpleStorageContractEncryptedPayloadHash, nil
}

func (sptm *StubPrivateTransactionManager) EncryptPayload(data []byte, from string, to []string, extra *engine.ExtraMetadata) ([]byte, error) {
	return nil, engine.ErrPrivateTxManagerNotSupported
}

func (sptm *StubPrivateTransactionManager) DecryptPayload(payload common.DecryptRequest) ([]byte, *engine.ExtraMetadata, error) {
	return nil, nil, engine.ErrPrivateTxManagerNotSupported
}

func (sptm *StubPrivateTransactionManager) StoreRaw(data []byte, from string) (common.EncryptedPayloadHash, error) {
	return arbitrarySimpleStorageContractEncryptedPayloadHash, nil
}

func (sptm *StubPrivateTransactionManager) SendSignedTx(data common.EncryptedPayloadHash, to []string, extra *engine.ExtraMetadata) (string, []string, []byte, error) {
	return "", nil, arbitrarySimpleStorageContractEncryptedPayloadHash.Bytes(), nil
}

func (sptm *StubPrivateTransactionManager) ReceiveRaw(data common.EncryptedPayloadHash) ([]byte, string, *engine.ExtraMetadata, error) {
	if sptm.creation {
		return hexutil.MustDecode("0x6060604052341561000f57600080fd5b604051602080610149833981016040528080519060200190919050505b806000819055505b505b610104806100456000396000f30060606040526000357c0100000000000000000000000000000000000000000000000000000000900463ffffffff1680632a1afcd914605157806360fe47b11460775780636d4ce63c146097575b600080fd5b3415605b57600080fd5b606160bd565b6040518082815260200191505060405180910390f35b3415608157600080fd5b6095600480803590602001909190505060c3565b005b341560a157600080fd5b60a760ce565b6040518082815260200191505060405180910390f35b60005481565b806000819055505b50565b6000805490505b905600a165627a7a72305820d5851baab720bba574474de3d09dbeaabc674a15f4dd93b974908476542c23f00029"), "", nil, nil
	} else {
		return hexutil.MustDecode("0x60fe47b1000000000000000000000000000000000000000000000000000000000000000e"), "", nil, nil
	}
}

func (sptm *StubPrivateTransactionManager) HasFeature(f engine.PrivateTransactionManagerFeature) bool {
	return true
}
