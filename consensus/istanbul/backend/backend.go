// Copyright 2017 The go-ethereum Authors
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

package backend

import (
	"crypto/ecdsa"
	"math/big"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	ibftcore "github.com/ethereum/go-ethereum/consensus/istanbul/ibft/core"
	ibftengine "github.com/ethereum/go-ethereum/consensus/istanbul/ibft/engine"
	qbftcore "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/core"
	qbftengine "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/engine"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/ethereum/go-ethereum/ethdb"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	lru "github.com/hashicorp/golang-lru"
)

const (
	// fetcherID is the ID indicates the block is from Istanbul engine
	fetcherID = "istanbul"
)

// New creates an Ethereum backend for Istanbul core engine.
func New(config *istanbul.Config, privateKey *ecdsa.PrivateKey, db ethdb.Database) *Backend {
	// Allocate the snapshot caches and create the engine
	recents, _ := lru.NewARC(inmemorySnapshots)
	recentMessages, _ := lru.NewARC(inmemoryPeers)
	knownMessages, _ := lru.NewARC(inmemoryMessages)

	sb := &Backend{
		config:           config,
		istanbulEventMux: new(event.TypeMux),
		privateKey:       privateKey,
		address:          crypto.PubkeyToAddress(privateKey.PublicKey),
		logger:           log.New(),
		db:               db,
		commitCh:         make(chan *types.Block, 1),
		recents:          recents,
		candidates:       make(map[common.Address]bool),
		coreStarted:      false,
		recentMessages:   recentMessages,
		knownMessages:    knownMessages,
	}

	sb.qbftEngine = qbftengine.NewEngine(sb.config, sb.address, sb.Sign)
	sb.ibftEngine = ibftengine.NewEngine(sb.config, sb.address, sb.Sign)

	return sb
}

// ----------------------------------------------------------------------------

type Backend struct {
	config *istanbul.Config

	privateKey *ecdsa.PrivateKey
	address    common.Address

	core istanbul.Core

	ibftEngine *ibftengine.Engine
	qbftEngine *qbftengine.Engine

	istanbulEventMux *event.TypeMux

	logger log.Logger

	db ethdb.Database

	chain        consensus.ChainHeaderReader
	currentBlock func() *types.Block
	hasBadBlock  func(hash common.Hash) bool

	// the channels for istanbul engine notifications
	commitCh          chan *types.Block
	proposedBlockHash common.Hash
	sealMu            sync.Mutex
	coreStarted       bool
	coreMu            sync.RWMutex

	// Current list of candidates we are pushing
	candidates map[common.Address]bool
	// Protects the signer fields
	candidatesLock sync.RWMutex
	// Snapshots for recent block to speed up reorgs
	recents *lru.ARCCache

	// event subscription for ChainHeadEvent event
	broadcaster consensus.Broadcaster

	recentMessages *lru.ARCCache // the cache of peer's messages
	knownMessages  *lru.ARCCache // the cache of self messages

	qbftConsensusEnabled bool // qbft consensus
}

func (sb *Backend) Engine() istanbul.Engine {
	return sb.EngineForBlockNumber(nil)
}

func (sb *Backend) EngineForBlockNumber(blockNumber *big.Int) istanbul.Engine {
	switch {
	case blockNumber != nil && sb.IsQBFTConsensusAt(blockNumber):
		return sb.qbftEngine
	case blockNumber == nil && sb.IsQBFTConsensus():
		return sb.qbftEngine
	default:
		return sb.ibftEngine
	}
}

// zekun: HACK
func (sb *Backend) CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int {
	return sb.EngineForBlockNumber(parent.Number).CalcDifficulty(chain, time, parent)
}

// Address implements istanbul.Backend.Address
func (sb *Backend) Address() common.Address {
	return sb.Engine().Address()
}

// Validators implements istanbul.Backend.Validators
func (sb *Backend) Validators(proposal istanbul.Proposal) istanbul.ValidatorSet {
	return sb.getValidators(proposal.Number().Uint64(), proposal.Hash())
}

// Broadcast implements istanbul.Backend.Broadcast
func (sb *Backend) Broadcast(valSet istanbul.ValidatorSet, code uint64, payload []byte) error {
	// send to others
	sb.Gossip(valSet, code, payload)
	// send to self
	msg := istanbul.MessageEvent{
		Code:    code,
		Payload: payload,
	}
	go sb.istanbulEventMux.Post(msg)
	return nil
}

// Gossip implements istanbul.Backend.Gossip
func (sb *Backend) Gossip(valSet istanbul.ValidatorSet, code uint64, payload []byte) error {
	hash := istanbul.RLPHash(payload)
	sb.knownMessages.Add(hash, true)

	targets := make(map[common.Address]bool)
	for _, val := range valSet.List() {
		if val.Address() != sb.Address() {
			targets[val.Address()] = true
		}
	}
	if sb.broadcaster != nil && len(targets) > 0 {
		ps := sb.broadcaster.FindPeers(targets)
		for addr, p := range ps {
			ms, ok := sb.recentMessages.Get(addr)
			var m *lru.ARCCache
			if ok {
				m, _ = ms.(*lru.ARCCache)
				if _, k := m.Get(hash); k {
					// This peer had this event, skip it
					continue
				}
			} else {
				m, _ = lru.NewARC(inmemoryMessages)
			}

			m.Add(hash, true)
			sb.recentMessages.Add(addr, m)

			if sb.IsQBFTConsensus() {
				var outboundCode uint64 = istanbulMsg
				if _, ok := qbfttypes.MessageCodes()[code]; ok {
					outboundCode = code
				}
				go p.SendQBFTConsensus(outboundCode, payload)
			} else {
				go p.SendConsensus(istanbulMsg, payload)
			}
		}
	}
	return nil
}

// Commit implements istanbul.Backend.Commit
func (sb *Backend) Commit(proposal istanbul.Proposal, seals [][]byte, round *big.Int) (err error) {
	// Check if the proposal is a valid block
	block, ok := proposal.(*types.Block)
	if !ok {
		sb.logger.Error("BFT: invalid block proposal", "proposal", proposal)
		return istanbulcommon.ErrInvalidProposal
	}

	// Commit header
	h := block.Header()
	err = sb.EngineForBlockNumber(h.Number).CommitHeader(h, seals, round)
	if err != nil {
		return
	}

	// Remove ValidatorSet added to ProposerPolicy registry, if not done, the registry keeps increasing size with each block height
	sb.config.ProposerPolicy.ClearRegistry()

	// update block's header
	block = block.WithSeal(h)

	sb.logger.Info("BFT: block proposal committed", "author", sb.Address(), "hash", proposal.Hash(), "number", proposal.Number().Uint64())

	// - if the proposed and committed blocks are the same, send the proposed hash
	//   to commit channel, which is being watched inside the engine.Seal() function.
	// - otherwise, we try to insert the block.
	// -- if success, the ChainHeadEvent event will be broadcasted, try to build
	//    the next block and the previous Seal() will be stopped.
	// -- otherwise, a error will be returned and a round change event will be fired.
	if sb.proposedBlockHash == block.Hash() {
		// feed block hash to Seal() and wait the Seal() result
		sb.commitCh <- block
		return nil
	}

	if sb.broadcaster != nil {
		sb.broadcaster.Enqueue(fetcherID, block)
	}

	return nil
}

// EventMux implements istanbul.Backend.EventMux
func (sb *Backend) EventMux() *event.TypeMux {
	return sb.istanbulEventMux
}

// Verify implements istanbul.Backend.Verify
func (sb *Backend) Verify(proposal istanbul.Proposal) (time.Duration, error) {
	// Check if the proposal is a valid block
	block, ok := proposal.(*types.Block)
	if !ok {
		sb.logger.Error("BFT: invalid block proposal", "proposal", proposal)
		return 0, istanbulcommon.ErrInvalidProposal
	}

	// check bad block
	if sb.HasBadProposal(block.Hash()) {
		sb.logger.Warn("BFT: bad block proposal", "proposal", proposal)
		return 0, core.ErrBlacklistedHash
	}

	header := block.Header()
	snap, err := sb.snapshot(sb.chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return 0, err
	}

	return sb.EngineForBlockNumber(header.Number).VerifyBlockProposal(sb.chain, block, snap.ValSet)
}

// Sign implements istanbul.Backend.Sign
func (sb *Backend) Sign(data []byte) ([]byte, error) {
	hashData := crypto.Keccak256(data)
	return crypto.Sign(hashData, sb.privateKey)
}

// SignWithoutHashing implements istanbul.Backend.SignWithoutHashing and signs input data with the backend's private key without hashing the input data
func (sb *Backend) SignWithoutHashing(data []byte) ([]byte, error) {
	return crypto.Sign(data, sb.privateKey)
}

// CheckSignature implements istanbul.Backend.CheckSignature
func (sb *Backend) CheckSignature(data []byte, address common.Address, sig []byte) error {
	signer, err := istanbul.GetSignatureAddress(data, sig)
	if err != nil {
		return err
	}
	// Compare derived addresses
	if signer != address {
		return istanbulcommon.ErrInvalidSignature
	}

	return nil
}

// HasPropsal implements istanbul.Backend.HashBlock
func (sb *Backend) HasPropsal(hash common.Hash, number *big.Int) bool {
	return sb.chain.GetHeader(hash, number.Uint64()) != nil
}

// GetProposer implements istanbul.Backend.GetProposer
func (sb *Backend) GetProposer(number uint64) common.Address {
	if h := sb.chain.GetHeaderByNumber(number); h != nil {
		a, _ := sb.Author(h)
		return a
	}
	return common.Address{}
}

// ParentValidators implements istanbul.Backend.GetParentValidators
func (sb *Backend) ParentValidators(proposal istanbul.Proposal) istanbul.ValidatorSet {
	if block, ok := proposal.(*types.Block); ok {
		return sb.getValidators(block.Number().Uint64()-1, block.ParentHash())
	}
	return validator.NewSet(nil, sb.config.ProposerPolicy)
}

func (sb *Backend) getValidators(number uint64, hash common.Hash) istanbul.ValidatorSet {
	snap, err := sb.snapshot(sb.chain, number, hash, nil)
	if err != nil {
		return validator.NewSet(nil, sb.config.ProposerPolicy)
	}
	return snap.ValSet
}

func (sb *Backend) LastProposal() (istanbul.Proposal, common.Address) {
	block := sb.currentBlock()

	var proposer common.Address
	if block.Number().Cmp(common.Big0) > 0 {
		var err error
		proposer, err = sb.Author(block.Header())
		if err != nil {
			sb.logger.Error("BFT: last block proposal invalid", "err", err)
			return nil, common.Address{}
		}
	}

	// Return header only block here since we don't need block body
	return block, proposer
}

func (sb *Backend) HasBadProposal(hash common.Hash) bool {
	if sb.hasBadBlock == nil {
		return false
	}
	return sb.hasBadBlock(hash)
}

func (sb *Backend) Close() error {
	return nil
}

// IsQBFTConsensus returns whether qbft consensus should be used
func (sb *Backend) IsQBFTConsensus() bool {
	if sb.qbftConsensusEnabled {
		return true
	}
	if sb.chain != nil {
		return sb.IsQBFTConsensusAt(sb.chain.CurrentHeader().Number)
	}
	return false
}

// IsQBFTConsensusForHeader checks if qbft consensus is enabled for the block height identified by the given header
func (sb *Backend) IsQBFTConsensusAt(blockNumber *big.Int) bool {
	return sb.config.IsQBFTConsensusAt(blockNumber)
}

func (sb *Backend) startIBFT() error {
	sb.logger.Info("BFT: activate IBFT")
	sb.logger.Trace("BFT: set ProposerPolicy sorter to ValidatorSortByStringFun")
	sb.config.ProposerPolicy.Use(istanbul.ValidatorSortByString())
	sb.qbftConsensusEnabled = false

	sb.core = ibftcore.New(sb, sb.config)
	if err := sb.core.Start(); err != nil {
		sb.logger.Error("BFT: failed to activate IBFT", "err", err)
		return err
	}

	return nil
}

func (sb *Backend) startQBFT() error {
	sb.logger.Info("BFT: activate QBFT")
	sb.logger.Trace("BFT: set ProposerPolicy sorter to ValidatorSortByByteFunc")
	sb.config.ProposerPolicy.Use(istanbul.ValidatorSortByByte())
	sb.qbftConsensusEnabled = true

	sb.core = qbftcore.New(sb, sb.config)
	if err := sb.core.Start(); err != nil {
		sb.logger.Error("BFT: failed to activate QBFT", "err", err)
		return err
	}

	return nil
}

func (sb *Backend) stop() error {
	core := sb.core
	sb.core = nil

	if core != nil {
		sb.logger.Info("BFT: deactivate")
		if err := core.Stop(); err != nil {
			sb.logger.Error("BFT: failed to deactivate", "err", err)
			return err
		}
	}

	sb.qbftConsensusEnabled = false

	return nil
}

// StartQBFTConsensus stops existing legacy ibft consensus and starts the new qbft consensus
func (sb *Backend) StartQBFTConsensus() error {
	sb.logger.Info("BFT: switch from IBFT to QBFT")
	if err := sb.stop(); err != nil {
		return err
	}

	return sb.startQBFT()
}
