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
	"bytes"
	"crypto/ecdsa"
	"math/big"
	"reflect"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul/testutils"
	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/rawdb"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
)

func newBlockchainFromConfig(genesis *core.Genesis, nodeKeys []*ecdsa.PrivateKey, cfg *istanbul.Config) (*core.BlockChain, *Backend) {
	memDB := rawdb.NewMemoryDatabase()

	// Use the first key as private key
	backend := New(cfg, nodeKeys[0], memDB)

	backend.qbftConsensusEnabled = backend.IsQBFTConsensus()
	genesis.MustCommit(memDB)

	blockchain, err := core.NewBlockChain(memDB, nil, genesis.Config, backend, vm.Config{}, nil, nil)
	if err != nil {
		panic(err)
	}

	backend.Start(blockchain, blockchain.CurrentBlock, blockchain.HasBadBlock)

	snap, err := backend.snapshot(blockchain, 0, common.Hash{}, nil)
	if err != nil {
		panic(err)
	}
	if snap == nil {
		panic("failed to get snapshot")
	}
	proposerAddr := snap.ValSet.GetProposer().Address()

	// find proposer key
	for _, key := range nodeKeys {
		addr := crypto.PubkeyToAddress(key.PublicKey)
		if addr.String() == proposerAddr.String() {
			backend.privateKey = key
			backend.address = addr
		}
	}

	return blockchain, backend
}

// in this test, we can set n to 1, and it means we can process Istanbul and commit a
// block by one node. Otherwise, if n is larger than 1, we have to generate
// other fake events to process Istanbul.
func newBlockChain(n int, qbftBlock *big.Int) (*core.BlockChain, *Backend) {
	isQBFT := qbftBlock != nil && qbftBlock.Uint64() == 0
	genesis, nodeKeys := testutils.GenesisAndKeys(n, isQBFT)

	config := copyConfig(istanbul.DefaultConfig)

	config.TestQBFTBlock = qbftBlock

	return newBlockchainFromConfig(genesis, nodeKeys, config)
}

// copyConfig create a copy of istanbul.Config, so that changing it does not update the original
func copyConfig(config *istanbul.Config) *istanbul.Config {
	cpy := *config
	return &cpy
}

func makeHeader(parent *types.Block, config *istanbul.Config) *types.Header {
	header := &types.Header{
		ParentHash: parent.Hash(),
		Number:     parent.Number().Add(parent.Number(), common.Big1),
		GasLimit:   core.CalcGasLimit(parent, parent.GasLimit(), parent.GasLimit()),
		GasUsed:    0,
		Time:       parent.Time() + config.BlockPeriod,
		Difficulty: istanbulcommon.DefaultDifficulty,
	}
	return header
}

func makeBlock(chain *core.BlockChain, engine *Backend, parent *types.Block) *types.Block {
	block := makeBlockWithoutSeal(chain, engine, parent)
	stopCh := make(chan struct{})
	resultCh := make(chan *types.Block, 10)
	go engine.Seal(chain, block, resultCh, stopCh)
	blk := <-resultCh
	return blk
}

func makeBlockWithoutSeal(chain *core.BlockChain, engine *Backend, parent *types.Block) *types.Block {
	header := makeHeader(parent, engine.config)
	engine.Prepare(chain, header)
	state, _, _ := chain.StateAt(parent.Root())
	block, _ := engine.FinalizeAndAssemble(chain, header, state, nil, nil, nil)
	return block
}

func TestIBFTPrepare(t *testing.T) {
	chain, engine := newBlockChain(1, nil)
	defer engine.Stop()
	chain.Config().Istanbul.TestQBFTBlock = nil
	header := makeHeader(chain.Genesis(), engine.config)
	err := engine.Prepare(chain, header)
	if err != nil {
		t.Errorf("error mismatch: have %v, want nil", err)
	}
	header.ParentHash = common.StringToHash("1234567890")
	err = engine.Prepare(chain, header)
	if err != consensus.ErrUnknownAncestor {
		t.Errorf("error mismatch: have %v, want %v", err, consensus.ErrUnknownAncestor)
	}
}

func TestQBFTPrepare(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()
	header := makeHeader(chain.Genesis(), engine.config)
	err := engine.Prepare(chain, header)
	if err != nil {
		t.Errorf("error mismatch: have %v, want nil", err)
	}

	header.ParentHash = common.StringToHash("1234567890")
	err = engine.Prepare(chain, header)
	if err != consensus.ErrUnknownAncestor {
		t.Errorf("error mismatch: have %v, want %v", err, consensus.ErrUnknownAncestor)
	}
}

func TestSealStopChannel(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()
	block := makeBlockWithoutSeal(chain, engine, chain.Genesis())
	stop := make(chan struct{}, 1)
	eventSub := engine.EventMux().Subscribe(istanbul.RequestEvent{})
	eventLoop := func() {
		ev := <-eventSub.Chan()
		_, ok := ev.Data.(istanbul.RequestEvent)
		if !ok {
			t.Errorf("unexpected event comes: %v", reflect.TypeOf(ev.Data))
		}
		stop <- struct{}{}
		eventSub.Unsubscribe()
	}
	go eventLoop()
	resultCh := make(chan *types.Block, 10)
	go func() {
		err := engine.Seal(chain, block, resultCh, stop)
		if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}
	}()

	finalBlock := <-resultCh
	if finalBlock != nil {
		t.Errorf("block mismatch: have %v, want nil", finalBlock)
	}
}

func TestSealCommittedOtherHash(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()
	block := makeBlockWithoutSeal(chain, engine, chain.Genesis())
	otherBlock := makeBlockWithoutSeal(chain, engine, block)
	expectedCommittedSeal := append([]byte{1, 2, 3}, bytes.Repeat([]byte{0x00}, types.IstanbulExtraSeal-3)...)

	eventSub := engine.EventMux().Subscribe(istanbul.RequestEvent{})
	blockOutputChannel := make(chan *types.Block)
	stopChannel := make(chan struct{})

	go func() {
		ev := <-eventSub.Chan()
		if _, ok := ev.Data.(istanbul.RequestEvent); !ok {
			t.Errorf("unexpected event comes: %v", reflect.TypeOf(ev.Data))
		}
		if err := engine.Commit(otherBlock, [][]byte{expectedCommittedSeal}, big.NewInt(0)); err != nil {
			t.Error(err.Error())
		}
		eventSub.Unsubscribe()
	}()

	go func() {
		if err := engine.Seal(chain, block, blockOutputChannel, stopChannel); err != nil {
			t.Error(err.Error())
		}
	}()

	select {
	case <-blockOutputChannel:
		t.Error("Wrong block found!")
	default:
		//no block found, stop the sealing
		close(stopChannel)
	}

	output := <-blockOutputChannel
	if output != nil {
		t.Error("Block not nil!")
	}
}

func updateQBFTBlock(block *types.Block, addr common.Address) *types.Block {
	header := block.Header()
	header.Coinbase = addr
	return block.WithSeal(header)
}

func TestSealCommitted(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()
	block := makeBlockWithoutSeal(chain, engine, chain.Genesis())
	expectedBlock := updateQBFTBlock(block, engine.Address())

	resultCh := make(chan *types.Block, 10)
	go func() {
		err := engine.Seal(chain, block, resultCh, make(chan struct{}))

		if err != nil {
			t.Errorf("error mismatch: have %v, want %v", err, expectedBlock)
		}
	}()

	finalBlock := <-resultCh
	if finalBlock.Hash() != expectedBlock.Hash() {
		t.Errorf("hash mismatch: have %v, want %v", finalBlock.Hash(), expectedBlock.Hash())
	}
}

func TestVerifyHeader(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()

	// istanbulcommon.ErrEmptyCommittedSeals case
	block := makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header := engine.chain.GetHeader(block.ParentHash(), block.NumberU64()-1)
	block = updateQBFTBlock(block, engine.Address())
	err := engine.VerifyHeader(chain, block.Header(), false)
	if err != istanbulcommon.ErrEmptyCommittedSeals {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrEmptyCommittedSeals)
	}

	// short extra data
	header = block.Header()
	header.Extra = []byte{}
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidExtraDataFormat {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidExtraDataFormat)
	}
	// incorrect extra format
	header.Extra = []byte("0000000000000000000000000000000012300000000000000000000000000000000000000000000000000000000000000000")
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidExtraDataFormat {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidExtraDataFormat)
	}

	// non zero MixDigest
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.MixDigest = common.StringToHash("123456789")
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidMixDigest {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidMixDigest)
	}

	// invalid uncles hash
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.UncleHash = common.StringToHash("123456789")
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidUncleHash {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidUncleHash)
	}

	// invalid difficulty
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.Difficulty = big.NewInt(2)
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidDifficulty {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidDifficulty)
	}

	// invalid timestamp
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.Time = chain.Genesis().Time() + (engine.config.BlockPeriod - 1)
	err = engine.VerifyHeader(chain, header, false)
	if err != istanbulcommon.ErrInvalidTimestamp {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidTimestamp)
	}

	// future block
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.Time = uint64(time.Now().Unix() + 10)
	err = engine.VerifyHeader(chain, header, false)
	if err != consensus.ErrFutureBlock {
		t.Errorf("error mismatch: have %v, want %v", err, consensus.ErrFutureBlock)
	}

	// future block which is within AllowedFutureBlockTime
	block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	header.Time = new(big.Int).Add(big.NewInt(time.Now().Unix()), new(big.Int).SetUint64(10)).Uint64()
	priorValue := engine.config.AllowedFutureBlockTime
	engine.config.AllowedFutureBlockTime = 10
	err = engine.VerifyHeader(chain, header, false)
	engine.config.AllowedFutureBlockTime = priorValue //restore changed value
	if err == consensus.ErrFutureBlock {
		t.Errorf("error mismatch: have %v, want nil", err)
	}

	// TODO This test does not make sense anymore as validate vote type is not stored in nonce
	// invalid nonce
	/*block = makeBlockWithoutSeal(chain, engine, chain.Genesis())
	header = block.Header()
	copy(header.Nonce[:], hexutil.MustDecode("0x111111111111"))
	header.Number = big.NewInt(int64(engine.config.Epoch))
	err = engine.VerifyHeader(chain, header, false)
	if err != errInvalidNonce {
		t.Errorf("error mismatch: have %v, want %v", err, errInvalidNonce)
	}*/
}

func TestVerifyHeaders(t *testing.T) {
	chain, engine := newBlockChain(1, big.NewInt(0))
	defer engine.Stop()
	genesis := chain.Genesis()

	// success case
	headers := []*types.Header{}
	blocks := []*types.Block{}
	size := 100

	for i := 0; i < size; i++ {
		var b *types.Block
		if i == 0 {
			b = makeBlockWithoutSeal(chain, engine, genesis)
			b = updateQBFTBlock(b, engine.Address())
		} else {
			b = makeBlockWithoutSeal(chain, engine, blocks[i-1])
			b = updateQBFTBlock(b, engine.Address())
		}
		blocks = append(blocks, b)
		headers = append(headers, blocks[i].Header())
	}
	// now = func() time.Time {
	// 	return time.Unix(int64(headers[size-1].Time), 0)
	// }
	_, results := engine.VerifyHeaders(chain, headers, nil)
	const timeoutDura = 2 * time.Second
	timeout := time.NewTimer(timeoutDura)
	index := 0
OUT1:
	for {
		select {
		case err := <-results:
			if err != nil {
				if err != istanbulcommon.ErrEmptyCommittedSeals && err != istanbulcommon.ErrInvalidCommittedSeals && err != consensus.ErrUnknownAncestor {
					t.Errorf("error mismatch: have %v, want istanbulcommon.ErrEmptyCommittedSeals|istanbulcommon.ErrInvalidCommittedSeals|ErrUnknownAncestor", err)
					break OUT1
				}
			}
			index++
			if index == size {
				break OUT1
			}
		case <-timeout.C:
			break OUT1
		}
	}
	_, results = engine.VerifyHeaders(chain, headers, nil)
	timeout = time.NewTimer(timeoutDura)
OUT2:
	for {
		select {
		case err := <-results:
			if err != nil {
				if err != istanbulcommon.ErrEmptyCommittedSeals && err != istanbulcommon.ErrInvalidCommittedSeals && err != consensus.ErrUnknownAncestor {
					t.Errorf("error mismatch: have %v, want istanbulcommon.ErrEmptyCommittedSeals|istanbulcommon.ErrInvalidCommittedSeals|ErrUnknownAncestor", err)
					break OUT2
				}
			}
		case <-timeout.C:
			break OUT2
		}
	}
	// error header cases
	headers[2].Number = big.NewInt(100)
	_, results = engine.VerifyHeaders(chain, headers, nil)
	timeout = time.NewTimer(timeoutDura)
	index = 0
	errors := 0
	expectedErrors := 0
OUT3:
	for {
		select {
		case err := <-results:
			if err != nil {
				if err != istanbulcommon.ErrEmptyCommittedSeals && err != istanbulcommon.ErrInvalidCommittedSeals && err != consensus.ErrUnknownAncestor {
					errors++
				}
			}
			index++
			if index == size {
				if errors != expectedErrors {
					t.Errorf("error mismatch: have %v, want %v", errors, expectedErrors)
				}
				break OUT3
			}
		case <-timeout.C:
			break OUT3
		}
	}
}
