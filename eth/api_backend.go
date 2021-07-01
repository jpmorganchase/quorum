// Copyright 2015 The go-ethereum Authors
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

package eth

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/bloombits"
	"github.com/ethereum/go-ethereum/core/mps"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/downloader"
	"github.com/ethereum/go-ethereum/eth/gasprice"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/miner"
	"github.com/ethereum/go-ethereum/params"
	pcore "github.com/ethereum/go-ethereum/permission/core"
	"github.com/ethereum/go-ethereum/rpc"
	"github.com/jpmorganchase/quorum-security-plugin-sdk-go/proto"
)

// EthAPIBackend implements ethapi.Backend for full nodes
type EthAPIBackend struct {
	extRPCEnabled bool
	eth           *Ethereum
	gpo           *gasprice.Oracle

	// Quorum
	//
	// hex node id from node public key
	hexNodeId string

	// timeout value for call
	evmCallTimeOut time.Duration
}

// ChainConfig returns the active chain configuration.
func (b *EthAPIBackend) ChainConfig() *params.ChainConfig {
	return b.eth.blockchain.Config()
}

// PSMR returns the private state metadata resolver.
func (b *EthAPIBackend) PSMR() mps.PrivateStateMetadataResolver {
	return b.eth.blockchain.PrivateStateManager()
}

func (b *EthAPIBackend) CurrentBlock() *types.Block {
	return b.eth.blockchain.CurrentBlock()
}

func (b *EthAPIBackend) SetHead(number uint64) {
	b.eth.protocolManager.downloader.Cancel()
	b.eth.blockchain.SetHead(number)
}

func (b *EthAPIBackend) HeaderByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Header, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		block := b.eth.miner.PendingBlock()
		return block.Header(), nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.eth.blockchain.CurrentBlock().Header(), nil
	}
	return b.eth.blockchain.GetHeaderByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) HeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.HeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		return header, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) HeaderByHash(ctx context.Context, hash common.Hash) (*types.Header, error) {
	return b.eth.blockchain.GetHeaderByHash(hash), nil
}

func (b *EthAPIBackend) BlockByNumber(ctx context.Context, number rpc.BlockNumber) (*types.Block, error) {
	// Pending block is only known by the miner
	if number == rpc.PendingBlockNumber {
		if b.eth.protocolManager.raftMode {
			// Use latest instead.
			return b.eth.blockchain.CurrentBlock(), nil
		}
		block := b.eth.miner.PendingBlock()
		return block, nil
	}
	// Otherwise resolve and return the block
	if number == rpc.LatestBlockNumber {
		return b.eth.blockchain.CurrentBlock(), nil
	}
	return b.eth.blockchain.GetBlockByNumber(uint64(number)), nil
}

func (b *EthAPIBackend) BlockByHash(ctx context.Context, hash common.Hash) (*types.Block, error) {
	return b.eth.blockchain.GetBlockByHash(hash), nil
}

func (b *EthAPIBackend) BlockByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (*types.Block, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.BlockByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header := b.eth.blockchain.GetHeaderByHash(hash)
		if header == nil {
			return nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, errors.New("hash is not currently canonical")
		}
		block := b.eth.blockchain.GetBlock(hash, header.Number.Uint64())
		if block == nil {
			return nil, errors.New("header found, but block body is missing")
		}
		return block, nil
	}
	return nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) StateAndHeaderByNumber(ctx context.Context, number rpc.BlockNumber) (vm.MinimalApiState, *types.Header, error) {
	psm, err := b.PSMR().ResolveForUserContext(ctx)
	if err != nil {
		return nil, nil, err
	}
	// Pending state is only known by the miner
	if number == rpc.PendingBlockNumber {
		// Quorum
		if b.eth.protocolManager.raftMode {
			// Use latest instead.
			header, err := b.HeaderByNumber(ctx, rpc.LatestBlockNumber)
			if header == nil || err != nil {
				return nil, nil, err
			}
			publicState, privateState, err := b.eth.BlockChain().StateAtPSI(header.Root, psm.ID)
			return EthAPIState{publicState, privateState}, header, err
		}
		block, publicState, privateState := b.eth.miner.Pending(psm.ID)
		return EthAPIState{publicState, privateState}, block.Header(), nil
	}
	// Otherwise resolve the block number and return its state
	header, err := b.HeaderByNumber(ctx, number)
	if err != nil {
		return nil, nil, err
	}
	if header == nil {
		return nil, nil, errors.New("header not found")
	}
	stateDb, privateState, err := b.eth.BlockChain().StateAtPSI(header.Root, psm.ID)
	return EthAPIState{stateDb, privateState}, header, err

}

func (b *EthAPIBackend) StateAndHeaderByNumberOrHash(ctx context.Context, blockNrOrHash rpc.BlockNumberOrHash) (vm.MinimalApiState, *types.Header, error) {
	if blockNr, ok := blockNrOrHash.Number(); ok {
		return b.StateAndHeaderByNumber(ctx, blockNr)
	}
	if hash, ok := blockNrOrHash.Hash(); ok {
		header, err := b.HeaderByHash(ctx, hash)
		if err != nil {
			return nil, nil, err
		}
		if header == nil {
			return nil, nil, errors.New("header for hash not found")
		}
		if blockNrOrHash.RequireCanonical && b.eth.blockchain.GetCanonicalHash(header.Number.Uint64()) != hash {
			return nil, nil, errors.New("hash is not currently canonical")
		}
		psm, err := b.PSMR().ResolveForUserContext(ctx)
		if err != nil {
			return nil, nil, err
		}
		stateDb, privateState, err := b.eth.BlockChain().StateAtPSI(header.Root, psm.ID)
		return EthAPIState{stateDb, privateState}, header, err

	}
	return nil, nil, errors.New("invalid arguments; neither block nor hash specified")
}

func (b *EthAPIBackend) GetReceipts(ctx context.Context, hash common.Hash) (types.Receipts, error) {
	receipts := b.eth.blockchain.GetReceiptsByHash(hash)
	psm, err := b.PSMR().ResolveForUserContext(ctx)
	if err != nil {
		return nil, err
	}

	psiReceipts := make([]*types.Receipt, len(receipts))
	for i := 0; i < len(receipts); i++ {
		psiReceipts[i] = receipts[i]
		if receipts[i].PSReceipts != nil {
			psReceipt, found := receipts[i].PSReceipts[psm.ID]
			if found {
				psiReceipts[i] = psReceipt
			}
		}
	}

	return psiReceipts, nil
}

func (b *EthAPIBackend) GetLogs(ctx context.Context, hash common.Hash) ([][]*types.Log, error) {
	receipts, err := b.GetReceipts(ctx, hash)
	if err != nil {
		return nil, err
	}
	if receipts == nil {
		return nil, nil
	}
	logs := make([][]*types.Log, len(receipts))
	for i, receipt := range receipts {
		logs[i] = receipt.Logs
	}
	return logs, nil
}

func (b *EthAPIBackend) GetTd(ctx context.Context, hash common.Hash) *big.Int {
	return b.eth.blockchain.GetTdByHash(hash)
}

func (b *EthAPIBackend) GetEVM(ctx context.Context, msg core.Message, state vm.MinimalApiState, header *types.Header) (*vm.EVM, func() error, error) {
	statedb := state.(EthAPIState)
	vmError := func() error { return nil }

	txContext := core.NewEVMTxContext(msg)
	context := core.NewEVMBlockContext(header, b.eth.BlockChain(), nil)

	// Set the private state to public state if contract address is not present in the private state
	to := common.Address{}
	if msg.To() != nil {
		to = *msg.To()
	}

	privateState := statedb.privateState
	if !privateState.Exist(to) {
		privateState = statedb.state
	}

	return vm.NewEVM(context, txContext, statedb.state, privateState, b.eth.blockchain.Config(), *b.eth.blockchain.GetVMConfig()), vmError, nil
}

func (b *EthAPIBackend) SubscribeRemovedLogsEvent(ch chan<- core.RemovedLogsEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeRemovedLogsEvent(ch)
}

func (b *EthAPIBackend) SubscribePendingLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.SubscribePendingLogs(ch) // Quorum
}

func (b *EthAPIBackend) SubscribeChainEvent(ch chan<- core.ChainEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainHeadEvent(ch chan<- core.ChainHeadEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainHeadEvent(ch)
}

func (b *EthAPIBackend) SubscribeChainSideEvent(ch chan<- core.ChainSideEvent) event.Subscription {
	return b.eth.BlockChain().SubscribeChainSideEvent(ch)
}

func (b *EthAPIBackend) SubscribeLogsEvent(ch chan<- []*types.Log) event.Subscription {
	return b.eth.BlockChain().SubscribeLogsEvent(ch)
}

func (b *EthAPIBackend) SendTx(ctx context.Context, signedTx *types.Transaction) error {
	// validation for node need to happen here and cannot be done as a part of
	// validateTx in tx_pool.go as tx_pool validation will happen in every node
	if b.hexNodeId != "" && !pcore.ValidateNodeForTxn(b.hexNodeId, signedTx.From()) {
		return errors.New("cannot send transaction from this node")
	}
	return b.eth.txPool.AddLocal(signedTx)
}

func (b *EthAPIBackend) GetPoolTransactions() (types.Transactions, error) {
	pending, err := b.eth.txPool.Pending()
	if err != nil {
		return nil, err
	}
	var txs types.Transactions
	for _, batch := range pending {
		txs = append(txs, batch...)
	}
	return txs, nil
}

func (b *EthAPIBackend) GetPoolTransaction(hash common.Hash) *types.Transaction {
	return b.eth.txPool.Get(hash)
}

func (b *EthAPIBackend) GetTransaction(ctx context.Context, txHash common.Hash) (*types.Transaction, common.Hash, uint64, uint64, error) {
	tx, blockHash, blockNumber, index := rawdb.ReadTransaction(b.eth.ChainDb(), txHash)
	return tx, blockHash, blockNumber, index, nil
}

func (b *EthAPIBackend) GetPoolNonce(ctx context.Context, addr common.Address) (uint64, error) {
	return b.eth.txPool.Nonce(addr), nil
}

func (b *EthAPIBackend) Stats() (pending int, queued int) {
	return b.eth.txPool.Stats()
}

func (b *EthAPIBackend) TxPoolContent() (map[common.Address]types.Transactions, map[common.Address]types.Transactions) {
	return b.eth.TxPool().Content()
}

func (b *EthAPIBackend) TxPool() *core.TxPool {
	return b.eth.TxPool()
}

func (b *EthAPIBackend) SubscribeNewTxsEvent(ch chan<- core.NewTxsEvent) event.Subscription {
	return b.eth.TxPool().SubscribeNewTxsEvent(ch)
}

func (b *EthAPIBackend) Downloader() *downloader.Downloader {
	return b.eth.Downloader()
}

func (b *EthAPIBackend) ProtocolVersion() int {
	return b.eth.EthVersion()
}

func (b *EthAPIBackend) SuggestPrice(ctx context.Context) (*big.Int, error) {
	if b.ChainConfig().IsQuorum {
		return big.NewInt(0), nil
	} else {
		return b.gpo.SuggestPrice(ctx)
	}
}

func (b *EthAPIBackend) ChainDb() ethdb.Database {
	return b.eth.ChainDb()
}

func (b *EthAPIBackend) EventMux() *event.TypeMux {
	return b.eth.EventMux()
}

func (b *EthAPIBackend) AccountManager() *accounts.Manager {
	return b.eth.AccountManager()
}

func (b *EthAPIBackend) ExtRPCEnabled() bool {
	return b.extRPCEnabled
}

func (b *EthAPIBackend) CallTimeOut() time.Duration {
	return b.evmCallTimeOut
}

func (b *EthAPIBackend) RPCGasCap() uint64 {
	return b.eth.config.RPCGasCap
}

func (b *EthAPIBackend) RPCTxFeeCap() float64 {
	return b.eth.config.RPCTxFeeCap
}

func (b *EthAPIBackend) BloomStatus() (uint64, uint64) {
	sections, _, _ := b.eth.bloomIndexer.Sections()
	return params.BloomBitsBlocks, sections
}

func (b *EthAPIBackend) ServiceFilter(ctx context.Context, session *bloombits.MatcherSession) {
	for i := 0; i < bloomFilterThreads; i++ {
		go session.Multiplex(bloomRetrievalBatch, bloomRetrievalWait, b.eth.bloomRequests)
	}
}

func (b *EthAPIBackend) Engine() consensus.Engine {
	return b.eth.engine
}

func (b *EthAPIBackend) CurrentHeader() *types.Header {
	return b.eth.blockchain.CurrentHeader()
}

func (b *EthAPIBackend) Miner() *miner.Miner {
	return b.eth.Miner()
}

func (b *EthAPIBackend) StartMining(threads int) error {
	return b.eth.StartMining(threads)
}

// The validation of pre-requisite for multitenancy is done when EthService
// is being created. So it's safe to use the config value here.
func (b *EthAPIBackend) SupportsMultitenancy(rpcCtx context.Context) (*proto.PreAuthenticatedAuthenticationToken, bool) {
	authToken := rpc.PreauthenticatedTokenFromContext(rpcCtx)
	if authToken != nil && b.eth.config.EnableMultitenancy {
		return authToken, true
	}
	return nil, false
}

func (b *EthAPIBackend) AccountExtraDataStateGetterByNumber(ctx context.Context, number rpc.BlockNumber) (vm.AccountExtraDataStateGetter, error) {
	s, _, err := b.StateAndHeaderByNumber(ctx, number)
	return s, err
}

// used by Quorum
type EthAPIState struct {
	state, privateState *state.StateDB
}

func (s EthAPIState) GetBalance(addr common.Address) *big.Int {
	if s.privateState.Exist(addr) {
		return s.privateState.GetBalance(addr)
	}
	return s.state.GetBalance(addr)
}

func (s EthAPIState) GetCode(addr common.Address) []byte {
	if s.privateState.Exist(addr) {
		return s.privateState.GetCode(addr)
	}
	return s.state.GetCode(addr)
}

func (s EthAPIState) SetNonce(addr common.Address, nonce uint64) {
	if s.privateState.Exist(addr) {
		s.privateState.SetNonce(addr, nonce)
	} else {
		s.state.SetNonce(addr, nonce)
	}
}

func (s EthAPIState) SetCode(addr common.Address, code []byte) {
	if s.privateState.Exist(addr) {
		s.privateState.SetCode(addr, code)
	} else {
		s.state.SetCode(addr, code)
	}
}

func (s EthAPIState) SetBalance(addr common.Address, balance *big.Int) {
	if s.privateState.Exist(addr) {
		s.privateState.SetBalance(addr, balance)
	} else {
		s.state.SetBalance(addr, balance)
	}
}

func (s EthAPIState) SetStorage(addr common.Address, storage map[common.Hash]common.Hash) {
	if s.privateState.Exist(addr) {
		s.privateState.SetStorage(addr, storage)
	} else {
		s.state.SetStorage(addr, storage)
	}
}

func (s EthAPIState) SetState(a common.Address, key common.Hash, value common.Hash) {
	if s.privateState.Exist(a) {
		s.privateState.SetState(a, key, value)
	} else {
		s.state.SetState(a, key, value)
	}
}

func (s EthAPIState) GetState(a common.Address, b common.Hash) common.Hash {
	if s.privateState.Exist(a) {
		return s.privateState.GetState(a, b)
	}
	return s.state.GetState(a, b)
}

func (s EthAPIState) GetNonce(addr common.Address) uint64 {
	if s.privateState.Exist(addr) {
		return s.privateState.GetNonce(addr)
	}
	return s.state.GetNonce(addr)
}

func (s EthAPIState) GetPrivacyMetadata(addr common.Address) (*state.PrivacyMetadata, error) {
	if s.privateState.Exist(addr) {
		return s.privateState.GetPrivacyMetadata(addr)
	}
	return nil, fmt.Errorf("%x: %w", addr, common.ErrNotPrivateContract)
}

func (s EthAPIState) GetManagedParties(addr common.Address) ([]string, error) {
	if s.privateState.Exist(addr) {
		return s.privateState.GetManagedParties(addr)
	}
	return nil, fmt.Errorf("%x: %w", addr, common.ErrNotPrivateContract)
}

func (s EthAPIState) GetRLPEncodedStateObject(addr common.Address) ([]byte, error) {
	getFunc := s.state.GetRLPEncodedStateObject
	if s.privateState.Exist(addr) {
		getFunc = s.privateState.GetRLPEncodedStateObject
	}
	return getFunc(addr)
}

func (s EthAPIState) GetProof(addr common.Address) ([][]byte, error) {
	if s.privateState.Exist(addr) {
		return s.privateState.GetProof(addr)
	}
	return s.state.GetProof(addr)
}

func (s EthAPIState) GetStorageProof(addr common.Address, h common.Hash) ([][]byte, error) {
	if s.privateState.Exist(addr) {
		return s.privateState.GetStorageProof(addr, h)
	}
	return s.state.GetStorageProof(addr, h)
}

func (s EthAPIState) StorageTrie(addr common.Address) state.Trie {
	if s.privateState.Exist(addr) {
		return s.privateState.StorageTrie(addr)
	}
	return s.state.StorageTrie(addr)
}

func (s EthAPIState) Error() error {
	if s.privateState.Error() != nil {
		return s.privateState.Error()
	}
	return s.state.Error()
}

func (s EthAPIState) GetCodeHash(addr common.Address) common.Hash {
	if s.privateState.Exist(addr) {
		return s.privateState.GetCodeHash(addr)
	}
	return s.state.GetCodeHash(addr)
}

//func (s MinimalApiState) Error
