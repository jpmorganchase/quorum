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
	"errors"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rpc"
)

// API is a user facing RPC API to dump Istanbul state
type API struct {
	chain   consensus.ChainHeaderReader
	backend *Backend
}

// BlockSigners is contains who created and who signed a particular block, denoted by its number and hash
type BlockSigners struct {
	Number     uint64
	Hash       common.Hash
	Author     common.Address
	Committers []common.Address
}

type Status struct {
	SigningStatus map[common.Address]int `json:"sealerActivity"`
	NumBlocks     uint64                 `json:"numBlocks"`
}

// NodeAddress returns the public address that is used to sign block headers in IBFT
func (api *API) NodeAddress() common.Address {
	return api.backend.Address()
}

// GetSignersFromBlock returns the signers and minter for a given block number, or the
// latest block available if none is specified
func (api *API) GetSignersFromBlock(number *rpc.BlockNumber) (*BlockSigners, error) {
	// Retrieve the requested block number (or current if none requested)
	var header *types.Header
	if number == nil || *number == rpc.LatestBlockNumber {
		header = api.chain.CurrentHeader()
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}

	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}

	return api.signers(header)
}

// GetSignersFromBlockByHash returns the signers and minter for a given block hash
func (api *API) GetSignersFromBlockByHash(hash common.Hash) (*BlockSigners, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}

	return api.signers(header)
}

func (api *API) signers(header *types.Header) (*BlockSigners, error) {
	author, err := api.backend.Author(header)
	if err != nil {
		return nil, err
	}

	committers, err := api.backend.Signers(header)
	if err != nil {
		return nil, err
	}

	return &BlockSigners{
		Number:     header.Number.Uint64(),
		Hash:       header.Hash(),
		Author:     author,
		Committers: committers,
	}, nil
}

// GetSnapshot retrieves the state snapshot at a given block.
func (api *API) GetSnapshot(number *rpc.BlockNumber) (*Snapshot, error) {
	// Retrieve the requested block number (or current if none requested)
	var header *types.Header
	if number == nil || *number == rpc.LatestBlockNumber {
		header = api.chain.CurrentHeader()
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}
	// Ensure we have an actually valid block and return its snapshot
	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}
	return api.backend.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
}

// GetSnapshotAtHash retrieves the state snapshot at a given block.
func (api *API) GetSnapshotAtHash(hash common.Hash) (*Snapshot, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}
	return api.backend.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
}

// GetValidators retrieves the list of authorized validators at the specified block.
func (api *API) GetValidators(number *rpc.BlockNumber) ([]common.Address, error) {
	// Retrieve the requested block number (or current if none requested)
	var header *types.Header
	if number == nil || *number == rpc.LatestBlockNumber {
		header = api.chain.CurrentHeader()
	} else {
		header = api.chain.GetHeaderByNumber(uint64(number.Int64()))
	}
	// Ensure we have an actually valid block and return the validators from its snapshot
	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}
	snap, err := api.backend.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return nil, err
	}
	return snap.validators(), nil
}

// GetValidatorsAtHash retrieves the state snapshot at a given block.
func (api *API) GetValidatorsAtHash(hash common.Hash) ([]common.Address, error) {
	header := api.chain.GetHeaderByHash(hash)
	if header == nil {
		return nil, istanbulcommon.ErrUnknownBlock
	}
	snap, err := api.backend.snapshot(api.chain, header.Number.Uint64(), header.Hash(), nil)
	if err != nil {
		return nil, err
	}
	return snap.validators(), nil
}

// Candidates returns the current candidates the node tries to uphold and vote on.
func (api *API) Candidates() map[common.Address]bool {
	api.backend.candidatesLock.RLock()
	defer api.backend.candidatesLock.RUnlock()

	proposals := make(map[common.Address]bool)
	for address, auth := range api.backend.candidates {
		proposals[address] = auth
	}
	return proposals
}

// Propose injects a new authorization candidate that the validator will attempt to
// push through.
func (api *API) Propose(address common.Address, auth bool) {
	api.backend.candidatesLock.Lock()
	defer api.backend.candidatesLock.Unlock()

	api.backend.candidates[address] = auth
}

// Discard drops a currently running candidate, stopping the validator from casting
// further votes (either for or against).
func (api *API) Discard(address common.Address) {
	api.backend.candidatesLock.Lock()
	defer api.backend.candidatesLock.Unlock()

	delete(api.backend.candidates, address)
}

func (api *API) Status(startBlockNum *rpc.BlockNumber, endBlockNum *rpc.BlockNumber) (*Status, error) {
	var (
		numBlocks   uint64
		header      = api.chain.CurrentHeader()
		start       uint64
		end         uint64
		blockNumber rpc.BlockNumber
	)
	if startBlockNum != nil && endBlockNum == nil {
		return nil, errors.New("pass the end block number")
	}

	if startBlockNum == nil && endBlockNum != nil {
		return nil, errors.New("pass the start block number")
	}

	if startBlockNum == nil && endBlockNum == nil {
		numBlocks = uint64(64)
		header = api.chain.CurrentHeader()
		end = header.Number.Uint64()
		start = end - numBlocks
		blockNumber = rpc.BlockNumber(header.Number.Int64())
	} else {
		end = uint64(*endBlockNum)
		start = uint64(*startBlockNum)
		if start > end {
			return nil, errors.New("start block number should be less than end block number")
		}

		if end > api.chain.CurrentHeader().Number.Uint64() {
			return nil, errors.New("end block number should be less than or equal to current block height")
		}

		numBlocks = end - start
		header = api.chain.GetHeaderByNumber(end)
		blockNumber = rpc.BlockNumber(end)
	}

	signers, err := api.GetValidators(&blockNumber)

	if err != nil {
		return nil, err
	}

	if numBlocks >= end {
		start = 1
		if end > start {
			numBlocks = end - start
		} else {
			numBlocks = 0
		}
	}
	signStatus := make(map[common.Address]int)
	for _, s := range signers {
		signStatus[s] = 0
	}

	for n := start; n < end; n++ {
		blockNum := rpc.BlockNumber(int64(n))
		s, _ := api.GetSignersFromBlock(&blockNum)
		signStatus[s.Author]++

	}
	return &Status{
		SigningStatus: signStatus,
		NumBlocks:     numBlocks,
	}, nil
}

func (api *API) IsValidator(blockNum *rpc.BlockNumber) (bool, error) {
	var blockNumber rpc.BlockNumber
	if blockNum != nil {
		blockNumber = *blockNum
	} else {
		header := api.chain.CurrentHeader()
		blockNumber = rpc.BlockNumber(header.Number.Int64())
	}
	s, _ := api.GetValidators(&blockNumber)

	for _, v := range s {
		if v == api.backend.address {
			return true, nil
		}
	}
	return false, nil
}
