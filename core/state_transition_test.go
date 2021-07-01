package core

import (
	"fmt"
	"math/big"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts/abi"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/math"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/params"
	"github.com/ethereum/go-ethereum/private"
	"github.com/ethereum/go-ethereum/private/engine"
	"github.com/ethereum/go-ethereum/private/engine/notinuse"
	"github.com/ethereum/go-ethereum/rlp"
	"github.com/ethereum/go-ethereum/trie"
	testifyassert "github.com/stretchr/testify/assert"
)

var (
	c1 = &contract{
		name:     "c1",
		abi:      mustParse(c1AbiDefinition),
		bytecode: common.Hex2Bytes("608060405234801561001057600080fd5b506040516020806105a88339810180604052602081101561003057600080fd5b81019080805190602001909291905050508060008190555050610550806100586000396000f3fe608060405260043610610051576000357c01000000000000000000000000000000000000000000000000000000009004806360fe47b1146100565780636d4ce63c146100a5578063d7139463146100d0575b600080fd5b34801561006257600080fd5b5061008f6004803603602081101561007957600080fd5b810190808035906020019092919050505061010b565b6040518082815260200191505060405180910390f35b3480156100b157600080fd5b506100ba61011e565b6040518082815260200191505060405180910390f35b3480156100dc57600080fd5b50610109600480360360208110156100f357600080fd5b8101908080359060200190929190505050610127565b005b6000816000819055506000549050919050565b60008054905090565b600030610132610212565b808273ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff168152602001915050604051809103906000f080158015610184573d6000803e3d6000fd5b5090508073ffffffffffffffffffffffffffffffffffffffff166360fe47b1836040518263ffffffff167c010000000000000000000000000000000000000000000000000000000002815260040180828152602001915050600060405180830381600087803b1580156101f657600080fd5b505af115801561020a573d6000803e3d6000fd5b505050505050565b604051610302806102238339019056fe608060405234801561001057600080fd5b506040516020806103028339810180604052602081101561003057600080fd5b8101908080519060200190929190505050806000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050610271806100916000396000f3fe608060405260043610610046576000357c01000000000000000000000000000000000000000000000000000000009004806360fe47b11461004b5780636d4ce63c14610086575b600080fd5b34801561005757600080fd5b506100846004803603602081101561006e57600080fd5b81019080803590602001909291905050506100b1565b005b34801561009257600080fd5b5061009b610180565b6040518082815260200191505060405180910390f35b6000809054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166360fe47b1826040518263ffffffff167c010000000000000000000000000000000000000000000000000000000002815260040180828152602001915050602060405180830381600087803b15801561014157600080fd5b505af1158015610155573d6000803e3d6000fd5b505050506040513d602081101561016b57600080fd5b81019080805190602001909291905050505050565b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16636d4ce63c6040518163ffffffff167c010000000000000000000000000000000000000000000000000000000002815260040160206040518083038186803b15801561020557600080fd5b505afa158015610219573d6000803e3d6000fd5b505050506040513d602081101561022f57600080fd5b810190808051906020019092919050505090509056fea165627a7a72305820a537f4c360ce5c6f55523298e314e6456e5c3e02c170563751dfda37d3aeddb30029a165627a7a7230582060396bfff29d2dfc5a9f4216bfba5e24d031d54fd4b26ebebde1a26c59df0c1e0029"),
	}
	c2 = &contract{
		name:     "c2",
		abi:      mustParse(c2AbiDefinition),
		bytecode: common.Hex2Bytes("608060405234801561001057600080fd5b506040516020806102f58339810180604052602081101561003057600080fd5b8101908080519060200190929190505050806000806101000a81548173ffffffffffffffffffffffffffffffffffffffff021916908373ffffffffffffffffffffffffffffffffffffffff16021790555050610264806100916000396000f3fe608060405234801561001057600080fd5b5060043610610053576000357c01000000000000000000000000000000000000000000000000000000009004806360fe47b1146100585780636d4ce63c14610086575b600080fd5b6100846004803603602081101561006e57600080fd5b81019080803590602001909291905050506100a4565b005b61008e610173565b6040518082815260200191505060405180910390f35b6000809054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff166360fe47b1826040518263ffffffff167c010000000000000000000000000000000000000000000000000000000002815260040180828152602001915050602060405180830381600087803b15801561013457600080fd5b505af1158015610148573d6000803e3d6000fd5b505050506040513d602081101561015e57600080fd5b81019080805190602001909291905050505050565b60008060009054906101000a900473ffffffffffffffffffffffffffffffffffffffff1673ffffffffffffffffffffffffffffffffffffffff16636d4ce63c6040518163ffffffff167c010000000000000000000000000000000000000000000000000000000002815260040160206040518083038186803b1580156101f857600080fd5b505afa15801561020c573d6000803e3d6000fd5b505050506040513d602081101561022257600080fd5b810190808051906020019092919050505090509056fea165627a7a72305820dd8a5dcf693e1969289c444a282d0684a9760bac26f1e4e0139d46821ec1979b0029"),
	}

	// exec hash helper vars (accounts/tries)
	signingAddress = common.StringToAddress("contract")

	c1AccAddress = crypto.CreateAddress(signingAddress, 0)
	c2AccAddress = crypto.CreateAddress(signingAddress, 1)

	// this is used as the field key in account storage (which is the index/sequence of the field in the contract)
	// both contracts have only one field (c1 - has the value while c2 has c1's address)
	// For more info please see: https://solidity.readthedocs.io/en/v0.6.8/internals/layout_in_storage.html
	firstFieldKey = common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000000")

	val42        = common.Hex2Bytes("000000000000000000000000000000000000000000000000000000000000002A")
	val53        = common.Hex2Bytes("0000000000000000000000000000000000000000000000000000000000000035")
	valC1Address = append(common.Hex2Bytes("000000000000000000000000"), c1AccAddress.Bytes()...)

	// this is the contract storage trie after storing value 42
	c1StorageTrieWithValue42   = secureTrieWithStoredValue(firstFieldKey, val42)
	c1StorageTrieWithValue53   = secureTrieWithStoredValue(firstFieldKey, val53)
	c2StorageTrieWithC1Address = secureTrieWithStoredValue(firstFieldKey, valC1Address)

	// The contract bytecode above includes the constructor bytecode (which is removed by the EVM before storing the
	// contract bytecode) thus it can't be used to calculate the code hash for the contract.
	// Below we deploy both of them as public contracts and extract the resulting codeHashes from the public state.
	c1CodeHash, c2CodeHash = contractCodeHashes()

	c1AccountWithValue42Stored   = &state.Account{Nonce: 1, Balance: big.NewInt(0), Root: c1StorageTrieWithValue42.Hash(), CodeHash: c1CodeHash.Bytes()}
	c1AccountWithValue53Stored   = &state.Account{Nonce: 1, Balance: big.NewInt(0), Root: c1StorageTrieWithValue53.Hash(), CodeHash: c1CodeHash.Bytes()}
	c2AccountWithC1AddressStored = &state.Account{Nonce: 1, Balance: big.NewInt(0), Root: c2StorageTrieWithC1Address.Hash(), CodeHash: c2CodeHash.Bytes()}
)

type contract struct {
	abi      abi.ABI
	bytecode []byte
	name     string
}

func (c *contract) create(args ...interface{}) []byte {
	bytes, err := c.abi.Pack("", args...)
	if err != nil {
		panic("can't pack: " + err.Error())
	}
	return append(c.bytecode, bytes...)
}

func (c *contract) set(value int64) []byte {
	bytes, err := c.abi.Pack("set", big.NewInt(value))
	if err != nil {
		panic("can't pack: " + err.Error())
	}
	return bytes
}

func (c *contract) get() []byte {
	bytes, err := c.abi.Pack("get")
	if err != nil {
		panic("can't pack: " + err.Error())
	}
	return bytes
}

func init() {
	log.PrintOrigins(true)
	log.Root().SetHandler(log.StreamHandler(os.Stdout, log.TerminalFormat(true)))
}

func secureTrieWithStoredValue(key []byte, value []byte) *trie.SecureTrie {
	res, _ := trie.NewSecure(common.Hash{}, trie.NewDatabase(rawdb.NewMemoryDatabase()))
	v, _ := rlp.EncodeToBytes(common.TrimLeftZeroes(value[:]))
	res.Update(key, v)
	return res
}

func contractCodeHashes() (c1CodeHash common.Hash, c2CodeHash common.Hash) {
	assert := testifyassert.New(nil)
	cfg := newConfig()

	// create public c1
	cfg.setData(c1.create(big.NewInt(42)))
	c1Address := createPublicContract(cfg, assert, c1)
	c1CodeHash = cfg.publicState.GetCodeHash(c1Address)

	// create public c2
	cfg.setNonce(1)
	cfg.setData(c2.create(c1Address))
	c2Address := createPublicContract(cfg, assert, c2)
	c2CodeHash = cfg.publicState.GetCodeHash(c2Address)

	return
}

func TestApplyMessage_Private_whenTypicalCreate_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)

	// calling C1.Create standard private
	cfg := newConfig().
		setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary encrypted payload hash"))
	gp := new(GasPool).AddGas(math.MaxUint64)
	privateMsg := newTypicalPrivateMessage(cfg)

	//since standard private create only get back PrivacyFlag
	mockPM.When("Receive").Return(c1.create(big.NewInt(42)), &engine.ExtraMetadata{
		PrivacyFlag: engine.PrivacyFlagStandardPrivate,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, gp)

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenCreatePartyProtectionC1_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)

	// calling C1.Create party protection
	cfg := newConfig().
		setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary encrypted payload hash"))
	gp := new(GasPool).AddGas(math.MaxUint64)
	privateMsg := newTypicalPrivateMessage(cfg)

	//since party protection create only get back privacyFlag
	mockPM.When("Receive").Return(c1.create(big.NewInt(42)), &engine.ExtraMetadata{
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, gp)

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenCreatePartyProtectionC1WithPrivacyEnhancementsDisabledReturnsError(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)

	// calling C1.Create party protection
	cfg := newConfig().
		setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary encrypted payload hash"))

	gp := new(GasPool).AddGas(math.MaxUint64)
	privateMsg := newTypicalPrivateMessage(cfg)

	//since party protection create only get back privacyFlag
	mockPM.When("Receive").Return(c1.create(big.NewInt(42)), &engine.ExtraMetadata{
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	evm := newEVM(cfg)
	evm.ChainConfig().PrivacyEnhancementsBlock = nil
	result, err := ApplyMessage(evm, privateMsg, gp)

	assert.Error(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	// check that there is no privacy metadata for the newly created contract
	assert.Len(evm.CreatedContracts(), 0, "no contracts created")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenInteractWithPartyProtectionC1_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	//create party protection c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	//since party protection need ACHashes and PrivacyFlag
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenInteractWithStateValidationC1_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	//create state validation c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c1EncPayloadHash)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue42Stored})
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set() state validation
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	mr, _ := calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue53Stored})
	//since state validation need ACHashes, MerkleRoot and PrivacyFlag
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag:  engine.PrivacyFlagStateValidation,
		ACMerkleRoot: mr,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenInteractWithStateValidationC1WithEmptyMRFromTessera_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create state validation c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c1EncPayloadHash)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue42Stored})
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set() state validation
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since state validation need ACHashes, privacyFlag, MerkleRoot
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag:  engine.PrivacyFlagStateValidation,
		ACMerkleRoot: common.Hash{},
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenInteractWithStateValidationC1WithWrongMRFromTessera_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	//create state validation c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c1EncPayloadHash)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue42Stored})
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set() state validation
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	//since state validation need ACHashes, PrivacyFlag, MerkleRoot
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag:  engine.PrivacyFlagStateValidation,
		ACMerkleRoot: common.Hash{123},
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

//Limitation of design --if don't send privacyFlag can't be guaranteed to catch non-party
//review this...
func TestApplyMessage_Private_whenNonPartyTriesInteractingWithPartyProtectionC1_NoFlag_Succeed(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	//act like doesnt exist on non-party node
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set()
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	//will have no ACHashes because when non-party sends tx, because no flag it doesn't generate privacyMetadata info
	//actual execution will find affected contract, but non-party won't have info
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes:    common.EncryptedPayloadHashes{},
		PrivacyFlag: engine.PrivacyFlagStandardPrivate,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenNonPartyTriesInteractingWithPartyProtectionC1_WithFlag_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	//act like doesnt exist on non-party node
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// calling C1.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(1).
		setTo(c1Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	mockPM.When("Receive").Return(c1.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

// C1 is a existing contract before privacy enhancements implementation
func TestApplyMessage_Private_whenPartyProtectionC2InteractsExistingStandardPrivateC1_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create c1 like c1 already exist before privacy enhancements
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(math.MaxUint64).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since party protection need ACHashes (only private non standard) and PrivacyFlag
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenPartyProtectionC2InteractsNewStandardPrivateC1_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create default standard private c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since party protection need ACHashes (only private non standard) and PrivacyFlag
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenPartyProtectionC2InteractsWithPartyProtectionC1_Succeed(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create party protection c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since party protection need ACHashes and PrivacyFlag
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

//scenario where sender Q1 runs simulation which affects c2 and c1 privy for Q3 and Q7
//Q3 receives block but wasn't privy to C1 so doesn't have creation info in tessera
func TestApplyMessage_Private_whenPartyProtectionC2AndC1ButMissingC1CreationInTessera_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create c1 as a party protection
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since party protection need ACHashes and PrivacyFlag
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

//scenario where the simulation is run on the Q1 (privatefor Q3 and Q7) and 3 contracts are affected (C2,C1,C0)
//but now Q3 receives block and should be privy to all 3 given tessera response
//but doesn't have C0 privacyMetadata stored in its db
// UPDATE - after relaxing the ACOTH checks this is a valid scenario where C0 acoth is ignored if it isn't detected as an
// affected contract during transaction execution
func TestApplyMessage_Private_whenPartyProtectionC2AndC1AndC0ButMissingC0InStateDB_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create party protection c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	c3EncPayloadHash := []byte("c3")
	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// since party protection need ACHashes and PrivacyFlag
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
			common.BytesToEncryptedPayloadHash(c3EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	// after ACOTH check updates this is a successful scenario
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenStateValidationC2InteractsWithStateValidationC1_Succeed(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create party protection c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c1EncPayloadHash)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue42Stored})
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create state validation c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c2EncPayloadHash).
		setNonce(1)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c2AccAddress, account: c2AccountWithC1AddressStored})
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() state validation
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)

	stuff := crypto.Keccak256Hash(c2.bytecode)
	log.Trace("stuff", "c2code", stuff[:])

	privateMsg := newTypicalPrivateMessage(cfg)
	mr, _ := calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue53Stored}, accEntry{address: c2AccAddress, account: c2AccountWithC1AddressStored})
	//since state validation need ACHashes, PrivacyFlag & MerkleRoot
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag:  engine.PrivacyFlagStateValidation,
		ACMerkleRoot: mr,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenStateValidationC2InteractsWithPartyProtectionC1_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create party protection c1
	c1EncPayloadHash := []byte("c1")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c1EncPayloadHash)
	c1Address := createContract(cfg, mockPM, assert, c1, big.NewInt(42))

	// create state validation c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData(c2EncPayloadHash).
		setNonce(1)
	cfg.acMerkleRoot, _ = calcAccMR(accEntry{address: c2AccAddress, account: c2AccountWithC1AddressStored})
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() state validation
	cfg.setPrivacyFlag(engine.PrivacyFlagStateValidation).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	// use the correctly calculated MR so that it can't be a source of false positives
	mr, _ := calcAccMR(accEntry{address: c1AccAddress, account: c1AccountWithValue53Stored}, accEntry{address: c2AccAddress, account: c2AccountWithC1AddressStored})
	//since state validation need ACHashes, PrivacyFlag & MerkleRoot
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
			common.BytesToEncryptedPayloadHash(c1EncPayloadHash): struct{}{},
		},
		PrivacyFlag:  engine.PrivacyFlagStateValidation,
		ACMerkleRoot: mr,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenStandardPrivateC2InteractsWithPublicC1_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create public c1
	cfg.setData(c1.create(big.NewInt(42)))
	c1Address := createPublicContract(cfg, assert, c1)

	// create standard private c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() standard private
	cfg.setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	//since standard private call no ACHashes, no MerkleRoot
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes:    common.EncryptedPayloadHashes{},
		PrivacyFlag: engine.PrivacyFlagStandardPrivate,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenPartyProtectionC2InteractsWithPublicC1_Fail(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)
	cfg := newConfig()

	// create public c1
	cfg.setData(c1.create(big.NewInt(42)))
	c1Address := createPublicContract(cfg, assert, c1)

	// create party protection c2
	c2EncPayloadHash := []byte("c2")
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData(c2EncPayloadHash).
		setNonce(1)
	c2Address := createContract(cfg, mockPM, assert, c2, c1Address)

	// calling C2.Set() party protection
	cfg.setPrivacyFlag(engine.PrivacyFlagPartyProtection).
		setData([]byte("arbitrary enc payload hash")).
		setNonce(2).
		setTo(c2Address)
	privateMsg := newTypicalPrivateMessage(cfg)
	mockPM.When("Receive").Return(c2.set(53), &engine.ExtraMetadata{
		ACHashes: common.EncryptedPayloadHashes{
			common.BytesToEncryptedPayloadHash(c2EncPayloadHash): struct{}{},
		},
		PrivacyFlag: engine.PrivacyFlagPartyProtection,
	}, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "EVM execution")
	assert.True(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenTxManagerReturnsError_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)

	// calling C1.Create standard private
	cfg := newConfig().
		setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary encrypted payload hash"))
	gp := new(GasPool).AddGas(math.MaxUint64)
	privateMsg := newTypicalPrivateMessage(cfg)

	//since standard private create only get back PrivacyFlag
	mockPM.When("Receive").Return(nil, nil, fmt.Errorf("Error during receive"))

	result, err := ApplyMessage(newEVM(cfg), privateMsg, gp)

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func TestApplyMessage_Private_whenTxManagerReturnsEmptyResult_Success(t *testing.T) {
	originalP := private.P
	defer func() { private.P = originalP }()
	mockPM := newMockPrivateTransactionManager()
	private.P = mockPM
	assert := testifyassert.New(t)

	// calling C1.Create standard private
	cfg := newConfig().
		setPrivacyFlag(engine.PrivacyFlagStandardPrivate).
		setData([]byte("arbitrary encrypted payload hash"))
	gp := new(GasPool).AddGas(math.MaxUint64)
	privateMsg := newTypicalPrivateMessage(cfg)

	//since standard private create only get back PrivacyFlag
	mockPM.When("Receive").Return(nil, nil, nil)

	result, err := ApplyMessage(newEVM(cfg), privateMsg, gp)

	assert.NoError(err, "EVM execution")
	assert.False(result.Failed(), "Transaction receipt status")
	mockPM.Verify(assert)
}

func createContract(cfg *config, mockPM *mockPrivateTransactionManager, assert *testifyassert.Assertions, c *contract, args ...interface{}) common.Address {
	defer mockPM.reset()

	privateMsg := newTypicalPrivateMessage(cfg)
	metadata := &engine.ExtraMetadata{}
	if cfg.privacyFlag < math.MaxUint64 {
		metadata.PrivacyFlag = cfg.privacyFlag
		if metadata.PrivacyFlag == engine.PrivacyFlagStateValidation {
			metadata.ACMerkleRoot = cfg.acMerkleRoot
		}
	}
	mockPM.When("Receive").Return(c.create(args...), metadata, nil)

	evm := newEVM(cfg)
	result, err := ApplyMessage(evm, privateMsg, new(GasPool).AddGas(math.MaxUint64))

	assert.NoError(err, "%s: EVM execution", c.name)
	assert.False(result.Failed(), "%s: Transaction receipt status", c.name)
	mockPM.Verify(assert)
	createdContracts := evm.CreatedContracts()
	log.Trace("priv statedb", "evmstatedb", evm.StateDB)
	assert.Len(createdContracts, 1, "%s: Number of created contracts", c.name)
	address := createdContracts[0]
	log.Debug("Created "+c.name, "address", address)
	return address
}

func createPublicContract(cfg *config, assert *testifyassert.Assertions, c *contract) common.Address {
	pubcfg := cfg.setPublicToPrivateState()
	msg := newTypicalPublicMessage(pubcfg)

	evm := newEVM(pubcfg)
	result, err := ApplyMessage(evm, msg, new(GasPool).AddGas(math.MaxUint64))
	assert.NoError(err, "%s: EVM execution", c.name)
	assert.False(result.Failed(), "%s: Transaction receipt status", c.name)
	createdContracts := evm.CreatedContracts()
	log.Trace("pub statedb", "evmstatedb", evm.StateDB)
	assert.Len(createdContracts, 1, "%s: Number of created contracts", c.name)
	address := createdContracts[0]
	log.Debug("Created "+c.name, "address", address)
	return address
}

func newTypicalPrivateMessage(cfg *config) PrivateMessage {
	var tx *types.Transaction
	if cfg.to == nil {
		tx = types.NewContractCreation(cfg.nonce, big.NewInt(0), math.MaxUint64, big.NewInt(0), cfg.data)
	} else {
		tx = types.NewTransaction(cfg.nonce, *cfg.to, big.NewInt(0), math.MaxUint64, big.NewInt(0), cfg.data)
	}
	tx.SetPrivate()
	if cfg.privacyFlag < math.MaxUint64 {
		tx.SetTxPrivacyMetadata(&types.PrivacyMetadata{
			PrivacyFlag: cfg.privacyFlag,
		})
	} else {
		tx.SetTxPrivacyMetadata(nil) // simulate standard private transaction
	}
	msg, err := tx.AsMessage(&stubSigner{})
	if err != nil {
		panic(fmt.Sprintf("can't create a new private message: %s", err))
	}
	cfg.currentTx = tx
	return PrivateMessage(msg)
}

func newTypicalPublicMessage(cfg *config) Message {
	var tx *types.Transaction
	if cfg.to == nil {
		tx = types.NewContractCreation(cfg.nonce, big.NewInt(0), math.MaxUint64, big.NewInt(0), cfg.data)
	} else {
		tx = types.NewTransaction(cfg.nonce, *cfg.to, big.NewInt(0), math.MaxUint64, big.NewInt(0), cfg.data)
	}
	tx.SetTxPrivacyMetadata(nil)
	msg, err := tx.AsMessage(&stubSigner{})
	if err != nil {
		panic(fmt.Sprintf("can't create a new private message: %s", err))
	}
	cfg.currentTx = tx
	return msg
}

type accEntry struct {
	address common.Address
	account *state.Account
}

func calcAccMR(entries ...accEntry) (common.Hash, error) {
	combined := new(trie.Trie)
	for _, entry := range entries {
		data, err := rlp.EncodeToBytes(entry.account)
		if err != nil {
			return common.Hash{}, err
		}
		if err = combined.TryUpdate(entry.address.Bytes(), data); err != nil {
			return common.Hash{}, err
		}
	}
	return combined.Hash(), nil
}

type config struct {
	from  common.Address
	to    *common.Address
	data  []byte
	nonce uint64

	privacyFlag  engine.PrivacyFlagType
	acMerkleRoot common.Hash

	currentTx *types.Transaction

	publicState, privateState *state.StateDB
}

func newConfig() *config {
	pubDatabase := rawdb.NewMemoryDatabase()
	privDatabase := rawdb.NewMemoryDatabase()
	publicState, _ := state.New(common.Hash{}, state.NewDatabase(pubDatabase), nil)
	privateState, _ := state.New(common.Hash{}, state.NewDatabase(privDatabase), nil)
	return &config{
		privateState: privateState,
		publicState:  publicState,
	}
}

func (cfg config) setPublicToPrivateState() *config {
	cfg.privateState = cfg.publicState
	return &cfg
}

func (cfg *config) setPrivacyFlag(f engine.PrivacyFlagType) *config {
	cfg.privacyFlag = f
	return cfg
}

func (cfg *config) setData(bytes []byte) *config {
	cfg.data = bytes
	return cfg
}

func (cfg *config) setNonce(n uint64) *config {
	cfg.nonce = n
	return cfg
}

func (cfg *config) setTo(address common.Address) *config {
	cfg.to = &address
	return cfg
}

func newEVM(cfg *config) *vm.EVM {
	context := vm.BlockContext{
		CanTransfer: CanTransfer,
		Transfer:    Transfer,
		GetHash:     func(uint64) common.Hash { return common.Hash{} },

		Coinbase:    common.Address{},
		BlockNumber: new(big.Int),
		Time:        big.NewInt(time.Now().Unix()),
		Difficulty:  new(big.Int),
		GasLimit:    uint64(3450366),
	}
	txContext := vm.TxContext{
		Origin:   common.Address{},
		GasPrice: big.NewInt(0),
	}
	evm := vm.NewEVM(context, txContext, cfg.publicState, cfg.privateState, &params.ChainConfig{
		ChainID:                  big.NewInt(1),
		ByzantiumBlock:           new(big.Int),
		HomesteadBlock:           new(big.Int),
		DAOForkBlock:             new(big.Int),
		DAOForkSupport:           false,
		EIP150Block:              new(big.Int),
		EIP155Block:              new(big.Int),
		EIP158Block:              new(big.Int),
		IsQuorum:                 true,
		PrivacyEnhancementsBlock: new(big.Int),
	}, vm.Config{})
	evm.SetCurrentTX(cfg.currentTx)
	return evm
}

func mustParse(def string) abi.ABI {
	ret, err := abi.JSON(strings.NewReader(def))
	if err != nil {
		panic(fmt.Sprintf("Can't parse ABI def %s", err))
	}
	return ret
}

type stubSigner struct {
}

func (ss *stubSigner) Sender(tx *types.Transaction) (common.Address, error) {
	return signingAddress, nil
}

func (ss *stubSigner) SignatureValues(tx *types.Transaction, sig []byte) (r, s, v *big.Int, err error) {
	panic("implement me")
}

func (ss *stubSigner) Hash(tx *types.Transaction) common.Hash {
	panic("implement me")
}

func (ss *stubSigner) Equal(types.Signer) bool {
	panic("implement me")
}

type mockPrivateTransactionManager struct {
	notinuse.PrivateTransactionManager
	returns       map[string][]interface{}
	currentMethod string
	count         map[string]int
}

func (mpm *mockPrivateTransactionManager) HasFeature(f engine.PrivateTransactionManagerFeature) bool {
	return true
}

func (mpm *mockPrivateTransactionManager) Receive(data common.EncryptedPayloadHash) (string, []string, []byte, *engine.ExtraMetadata, error) {
	mpm.count["Receive"]++
	values := mpm.returns["Receive"]
	var (
		r1 []byte
		r2 *engine.ExtraMetadata
		r3 error
	)
	if values[0] != nil {
		r1 = values[0].([]byte)
	}
	if values[1] != nil {
		r2 = values[1].(*engine.ExtraMetadata)
	}
	if values[2] != nil {
		r3 = values[2].(error)
	}
	return "", nil, r1, r2, r3
}

func (mpm *mockPrivateTransactionManager) When(name string) *mockPrivateTransactionManager {
	mpm.currentMethod = name
	mpm.count[name] = -1
	return mpm
}

func (mpm *mockPrivateTransactionManager) Return(values ...interface{}) {
	mpm.returns[mpm.currentMethod] = values
}

func (mpm *mockPrivateTransactionManager) Verify(assert *testifyassert.Assertions) {
	for m, c := range mpm.count {
		assert.True(c > -1, "%s has not been called", m)
	}
}

func (mpm *mockPrivateTransactionManager) reset() {
	mpm.count = make(map[string]int)
	mpm.currentMethod = ""
	mpm.returns = make(map[string][]interface{})
}

func newMockPrivateTransactionManager() *mockPrivateTransactionManager {
	return &mockPrivateTransactionManager{
		returns: make(map[string][]interface{}),
		count:   make(map[string]int),
	}
}

const (
	c1AbiDefinition = `
[
	{
		"constant": false,
		"inputs": [
			{
				"name": "newValue",
				"type": "uint256"
			}
		],
		"name": "set",
		"outputs": [
			{
				"name": "",
				"type": "uint256"
			}
		],
		"payable": false,
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"constant": true,
		"inputs": [],
		"name": "get",
		"outputs": [
			{
				"name": "",
				"type": "uint256"
			}
		],
		"payable": false,
		"stateMutability": "view",
		"type": "function"
	},
	{
		"constant": false,
		"inputs": [
			{
				"name": "newValue",
				"type": "uint256"
			}
		],
		"name": "newContractC2",
		"outputs": [],
		"payable": false,
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"inputs": [
			{
				"name": "initVal",
				"type": "uint256"
			}
		],
		"payable": false,
		"stateMutability": "nonpayable",
		"type": "constructor"
	}
]
`
	c2AbiDefinition = `
[
	{
		"constant": false,
		"inputs": [
			{
				"name": "_val",
				"type": "uint256"
			}
		],
		"name": "set",
		"outputs": [],
		"payable": false,
		"stateMutability": "nonpayable",
		"type": "function"
	},
	{
		"constant": true,
		"inputs": [],
		"name": "get",
		"outputs": [
			{
				"name": "result",
				"type": "uint256"
			}
		],
		"payable": false,
		"stateMutability": "view",
		"type": "function"
	},
	{
		"inputs": [
			{
				"name": "_t",
				"type": "address"
			}
		],
		"payable": false,
		"stateMutability": "nonpayable",
		"type": "constructor"
	}
]
`
)

func verifyGasPoolCalculation(t *testing.T, pm private.PrivateTransactionManager) {
	assert := testifyassert.New(t)
	saved := private.P
	defer func() {
		private.P = saved
	}()
	private.P = pm

	txGasLimit := uint64(100000)
	gasPool := new(GasPool).AddGas(200000)
	// this payload would give us 25288 intrinsic gas
	arbitraryEncryptedPayload := "4ab80888354582b92ab442a317828386e4bf21ea4a38d1a9183fbb715f199475269d7686939017f4a6b28310d5003ebd8e012eade530b79e157657ce8dd9692a"
	expectedGasPool := new(GasPool).AddGas(177988) // only intrinsic gas is deducted

	db := rawdb.NewMemoryDatabase()
	privateState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	publicState, _ := state.New(common.Hash{}, state.NewDatabase(db), nil)
	msg := privateCallMsg{
		callmsg: callmsg{
			addr:     common.Address{2},
			to:       &common.Address{},
			value:    new(big.Int),
			gas:      txGasLimit,
			gasPrice: big.NewInt(0),
			data:     common.Hex2Bytes(arbitraryEncryptedPayload),
		},
	}
	ctx := NewEVMBlockContext(&dualStateTestHeader, nil, &common.Address{})
	txCtx := NewEVMTxContext(msg)
	evm := vm.NewEVM(ctx, txCtx, publicState, privateState, params.QuorumTestChainConfig, vm.Config{})

	tx := types.NewTransaction(
		0,
		common.Address{},
		big.NewInt(0),
		txGasLimit,
		big.NewInt(0),
		common.Hex2Bytes(arbitraryEncryptedPayload))
	evm.SetCurrentTX(tx)

	arbitraryBalance := big.NewInt(100000000)
	publicState.SetBalance(evm.Context.Coinbase, arbitraryBalance)
	publicState.SetBalance(msg.From(), arbitraryBalance)

	testObject := NewStateTransition(evm, msg, gasPool)

	result, err := testObject.TransitionDb()

	assert.NoError(err)
	assert.False(result.Failed())

	assert.Equal(new(big.Int).SetUint64(expectedGasPool.Gas()), new(big.Int).SetUint64(gasPool.Gas()), "gas pool must be calculated correctly")
	assert.Equal(arbitraryBalance, publicState.GetBalance(evm.Context.Coinbase), "balance must not be changed")
	assert.Equal(arbitraryBalance, publicState.GetBalance(msg.From()), "balance must not be changed")
}

func TestStateTransition_TransitionDb_GasPoolCalculation_whenNonPartyNodeProcessingPrivateTransactions(t *testing.T) {
	stubPTM := &StubPrivateTransactionManager{
		responses: map[string][]interface{}{
			"Receive": {
				[]byte{},
				nil,
			},
		},
	}
	verifyGasPoolCalculation(t, stubPTM)
}

func TestStateTransition_TransitionDb_GasPoolCalculation_whenPartyNodeProcessingPrivateTransactions(t *testing.T) {
	stubPTM := &StubPrivateTransactionManager{
		responses: map[string][]interface{}{
			"Receive": {
				common.Hex2Bytes("600a6000526001601ff300"),
				nil,
			},
		},
	}
	verifyGasPoolCalculation(t, stubPTM)
}

type privateCallMsg struct {
	callmsg
}

func (pm privateCallMsg) IsPrivate() bool { return true }

type StubPrivateTransactionManager struct {
	notinuse.PrivateTransactionManager
	responses map[string][]interface{}
}

func (spm *StubPrivateTransactionManager) Receive(data common.EncryptedPayloadHash) (string, []string, []byte, *engine.ExtraMetadata, error) {
	res := spm.responses["Receive"]
	if err, ok := res[1].(error); ok {
		return "", nil, nil, nil, err
	}
	if ret, ok := res[0].([]byte); ok {
		return "", nil, ret, &engine.ExtraMetadata{
			PrivacyFlag: engine.PrivacyFlagStandardPrivate,
		}, nil
	}
	return "", nil, nil, nil, nil
}

func (spm *StubPrivateTransactionManager) ReceiveRaw(hash common.EncryptedPayloadHash) ([]byte, string, *engine.ExtraMetadata, error) {
	_, sender, data, metadata, err := spm.Receive(hash)
	return data, sender[0], metadata, err
}

func (spm *StubPrivateTransactionManager) HasFeature(f engine.PrivateTransactionManagerFeature) bool {
	return true
}
