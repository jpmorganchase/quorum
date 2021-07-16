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
	"math/big"
	"math/rand"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rpc"
)

const (
	checkpointInterval = 1024 // Number of blocks after which to save the vote snapshot to the database
	inmemorySnapshots  = 128  // Number of recent vote snapshots to keep in memory
	inmemoryPeers      = 40
	inmemoryMessages   = 1024
)

// Author retrieves the Ethereum address of the account that minted the given
// block, which may be different from the header's coinbase if a consensus
// engine is based on signatures.
func (sb *Backend) Author(header *types.Header) (common.Address, error) {
	return sb.EngineForBlockNumber(header.Number).Author(header)
}

// Signers extracts all the addresses who have signed the given header
// It will extract for each seal who signed it, regardless of if the seal is
// repeated
func (sb *Backend) Signers(header *types.Header) ([]common.Address, error) {
	return sb.EngineForBlockNumber(header.Number).Signers(header)
}

// VerifyHeader checks whether a header conforms to the consensus rules of a
// given engine. Verifying the seal may be done optionally here, or explicitly
// via the VerifySeal method.
func (sb *Backend) VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, seal bool) error {
	return sb.verifyHeader(chain, header, nil)
}

func (sb *Backend) verifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header) error {
	// Assemble the voting snapshot
	snap, err := sb.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, parents)
	if err != nil {
		return err
	}

	return sb.EngineForBlockNumber(header.Number).VerifyHeader(chain, header, parents, snap.ValSet)
}

// VerifyHeaders is similar to VerifyHeader, but verifies a batch of headers
// concurrently. The method returns a quit channel to abort the operations and
// a results channel to retrieve the async verifications (the order is that of
// the input slice).
func (sb *Backend) VerifyHeaders(chain consensus.ChainHeaderReader, headers []*types.Header, seals []bool) (chan<- struct{}, <-chan error) {
	abort := make(chan struct{})
	results := make(chan error, len(headers))
	go func() {
		errored := false
		for i, header := range headers {
			var err error
			if errored {
				err = consensus.ErrUnknownAncestor
			} else {
				err = sb.verifyHeader(chain, header, headers[:i])
			}

			if err != nil {
				errored = true
			}

			select {
			case <-abort:
				return
			case results <- err:
			}
		}
	}()
	return abort, results
}

// VerifyUncles verifies that the given block's uncles conform to the consensus
// rules of a given engine.
func (sb *Backend) VerifyUncles(chain consensus.ChainReader, block *types.Block) error {
	return sb.EngineForBlockNumber(block.Header().Number).VerifyUncles(chain, block)
}

// VerifySeal checks whether the crypto seal on a header is valid according to
// the consensus rules of the given engine.
func (sb *Backend) VerifySeal(chain consensus.ChainHeaderReader, header *types.Header) error {
	// get parent header and ensure the signer is in parent's validator set
	number := header.Number.Uint64()
	if number == 0 {
		return istanbulcommon.ErrUnknownBlock
	}

	// Assemble the voting snapshot
	snap, err := sb.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	return sb.EngineForBlockNumber(header.Number).VerifySeal(chain, header, snap.ValSet)
}

// Prepare initializes the consensus fields of a block header according to the
// rules of a particular engine. The changes are executed inline.
func (sb *Backend) Prepare(chain consensus.ChainHeaderReader, header *types.Header) error {
	// Assemble the voting snapshot
	snap, err := sb.snapshot(chain, header.Number.Uint64()-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	err = sb.EngineForBlockNumber(header.Number).Prepare(chain, header, snap.ValSet)
	if err != nil {
		return err
	}

	// get valid candidate list
	sb.candidatesLock.RLock()
	var addresses []common.Address
	var authorizes []bool
	for address, authorize := range sb.candidates {
		if snap.checkVote(address, authorize) {
			addresses = append(addresses, address)
			authorizes = append(authorizes, authorize)
		}
	}
	sb.candidatesLock.RUnlock()

	if len(addresses) > 0 {
		index := rand.Intn(len(addresses))

		err = sb.EngineForBlockNumber(header.Number).WriteVote(header, addresses[index], authorizes[index])
		if err != nil {
			log.Error("BFT: error writing validator vote", "err", err)
			return err
		}
	}

	return nil
}

// Finalize runs any post-transaction state modifications (e.g. block rewards)
// and assembles the final block.
//
// Note, the block header and state database might be updated to reflect any
// consensus rules that happen at finalization (e.g. block rewards).
func (sb *Backend) Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header) {
	sb.EngineForBlockNumber(header.Number).Finalize(chain, header, state, txs, uncles)
}

// FinalizeAndAssemble implements consensus.Engine, ensuring no uncles are set,
// nor block rewards given, and returns the final block.
func (sb *Backend) FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error) {
	return sb.EngineForBlockNumber(header.Number).FinalizeAndAssemble(chain, header, state, txs, uncles, receipts)
}

// Seal generates a new block for the given input block with the local miner's
// seal place on top.
func (sb *Backend) Seal(chain consensus.ChainHeaderReader, block *types.Block, results chan<- *types.Block, stop <-chan struct{}) error {
	// update the block header timestamp and signature and propose the block to core engine
	header := block.Header()
	number := header.Number.Uint64()

	// Bail out if we're unauthorized to sign a block
	snap, err := sb.snapshot(chain, number-1, header.ParentHash, nil)
	if err != nil {
		return err
	}

	block, err = sb.EngineForBlockNumber(header.Number).Seal(chain, block, snap.ValSet)
	if err != nil {
		return err
	}

	delay := time.Until(time.Unix(int64(block.Header().Time), 0))

	go func() {
		// wait for the timestamp of header, use this to adjust the block period
		select {
		case <-time.After(delay):
		case <-stop:
			results <- nil
			return
		}

		// get the proposed block hash and clear it if the seal() is completed.
		sb.sealMu.Lock()
		sb.proposedBlockHash = block.Hash()

		defer func() {
			sb.proposedBlockHash = common.Hash{}
			sb.sealMu.Unlock()
		}()
		// post block into Istanbul engine
		go sb.EventMux().Post(istanbul.RequestEvent{
			Proposal: block,
		})
		for {
			select {
			case result := <-sb.commitCh:
				// if the block hash and the hash from channel are the same,
				// return the result. Otherwise, keep waiting the next hash.
				if result != nil && block.Hash() == result.Hash() {
					results <- result
					return
				}
			case <-stop:
				results <- nil
				return
			}
		}
	}()
	return nil
}

// APIs returns the RPC APIs this consensus engine provides.
func (sb *Backend) APIs(chain consensus.ChainHeaderReader) []rpc.API {
	return []rpc.API{{
		Namespace: "istanbul",
		Version:   "1.0",
		Service:   &API{chain: chain, backend: sb},
		Public:    true,
	}}
}

// Start implements consensus.Istanbul.Start
func (sb *Backend) Start(chain consensus.ChainHeaderReader, currentBlock func() *types.Block, hasBadBlock func(hash common.Hash) bool) error {
	sb.coreMu.Lock()
	defer sb.coreMu.Unlock()
	if sb.coreStarted {
		return istanbul.ErrStartedEngine
	}

	// clear previous data
	sb.proposedBlockHash = common.Hash{}
	if sb.commitCh != nil {
		close(sb.commitCh)
	}
	sb.commitCh = make(chan *types.Block, 1)

	sb.chain = chain
	sb.currentBlock = currentBlock
	sb.hasBadBlock = hasBadBlock

	// Check if qbft Consensus needs to be used after chain is set
	var err error
	if sb.IsQBFTConsensus() {
		err = sb.startQBFT()
	} else {
		err = sb.startIBFT()
	}

	if err != nil {
		return err
	}

	sb.coreStarted = true

	return nil
}

// Stop implements consensus.Istanbul.Stop
func (sb *Backend) Stop() error {
	sb.coreMu.Lock()
	defer sb.coreMu.Unlock()
	if !sb.coreStarted {
		return istanbul.ErrStoppedEngine
	}
	if err := sb.stop(); err != nil {
		return err
	}
	sb.coreStarted = false

	return nil
}

func addrsToString(addrs []common.Address) []string {
	strs := make([]string, len(addrs))
	for i, addr := range addrs {
		strs[i] = addr.String()
	}
	return strs
}

func (sb *Backend) snapLogger(snap *Snapshot) log.Logger {
	return sb.logger.New(
		"snap.number", snap.Number,
		"snap.hash", snap.Hash.String(),
		"snap.epoch", snap.Epoch,
		"snap.validators", addrsToString(snap.validators()),
		"snap.votes", snap.Votes,
	)
}

func (sb *Backend) storeSnap(snap *Snapshot) error {
	logger := sb.snapLogger(snap)
	logger.Debug("BFT: store snapshot to database")
	if err := snap.store(sb.db); err != nil {
		logger.Error("BFT: failed to store snapshot to database", "err", err)
		return err
	}

	return nil
}

// snapshot retrieves the authorization snapshot at a given point in time.
func (sb *Backend) snapshot(chain consensus.ChainHeaderReader, number uint64, hash common.Hash, parents []*types.Header) (*Snapshot, error) {
	// Search for a snapshot in memory or on disk for checkpoints
	var (
		headers []*types.Header
		snap    *Snapshot
	)
	for snap == nil {
		// If an in-memory snapshot was found, use that
		if s, ok := sb.recents.Get(hash); ok {
			snap = s.(*Snapshot)
			sb.snapLogger(snap).Trace("BFT: loaded voting snapshot from cache")
			break
		}
		// If an on-disk checkpoint snapshot can be found, use that
		if number%checkpointInterval == 0 {
			if s, err := loadSnapshot(sb.config.Epoch, sb.db, hash); err == nil {
				snap = s
				sb.snapLogger(snap).Trace("BFT: loaded voting snapshot from database")
				break
			}
		}

		// If we're at block zero, make a snapshot
		if number == 0 {
			genesis := chain.GetHeaderByNumber(0)
			if err := sb.EngineForBlockNumber(big.NewInt(0)).VerifyHeader(chain, genesis, nil, nil); err != nil {
				sb.logger.Error("BFT: invalid genesis block", "err", err)
				return nil, err
			}

			// Get the validators from genesis to create a snapshot
			validators, err := sb.EngineForBlockNumber(big.NewInt(0)).Validators(genesis)
			if err != nil {
				sb.logger.Error("BFT: invalid genesis block", "err", err)
				return nil, err
			}

			snap = newSnapshot(sb.config.Epoch, 0, genesis.Hash(), validator.NewSet(validators, sb.config.ProposerPolicy))
			if err := sb.storeSnap(snap); err != nil {
				return nil, err
			}
			break
		}

		// No snapshot for this header, gather the header and move backward
		var header *types.Header
		if len(parents) > 0 {
			// If we have explicit parents, pick from there (enforced)
			header = parents[len(parents)-1]
			if header.Hash() != hash || header.Number.Uint64() != number {
				return nil, consensus.ErrUnknownAncestor
			}
			parents = parents[:len(parents)-1]
		} else {
			// No explicit parents (or no more left), reach out to the database
			header = chain.GetHeader(hash, number)
			if header == nil {
				return nil, consensus.ErrUnknownAncestor
			}
		}

		headers = append(headers, header)
		number, hash = number-1, header.ParentHash
	}

	// Previous snapshot found, apply any pending headers on top of it
	for i := 0; i < len(headers)/2; i++ {
		headers[i], headers[len(headers)-1-i] = headers[len(headers)-1-i], headers[i]
	}

	snap, err := sb.snapApply(snap, headers)
	if err != nil {
		return nil, err
	}
	sb.recents.Add(snap.Hash, snap)

	// If we've generated a new checkpoint snapshot, save to disk
	if snap.Number%checkpointInterval == 0 && len(headers) > 0 {
		if err = sb.storeSnap(snap); err != nil {
			return nil, err
		}
	}

	return snap, err
}

// SealHash returns the hash of a block prior to it being sealed.
func (sb *Backend) SealHash(header *types.Header) common.Hash {
	return sb.EngineForBlockNumber(header.Number).SealHash(header)
}

func (sb *Backend) snapApply(snap *Snapshot, headers []*types.Header) (*Snapshot, error) {
	// Allow passing in no headers for cleaner code
	if len(headers) == 0 {
		return snap, nil
	}
	// Sanity check that the headers can be applied
	for i := 0; i < len(headers)-1; i++ {
		if headers[i+1].Number.Uint64() != headers[i].Number.Uint64()+1 {
			return nil, istanbulcommon.ErrInvalidVotingChain
		}
	}
	if headers[0].Number.Uint64() != snap.Number+1 {
		return nil, istanbulcommon.ErrInvalidVotingChain
	}
	// Iterate through the headers and create a new snapshot
	snapCpy := snap.copy()

	for _, header := range headers {
		err := sb.snapApplyHeader(snapCpy, header)
		if err != nil {
			return nil, err
		}
	}
	snapCpy.Number += uint64(len(headers))
	snapCpy.Hash = headers[len(headers)-1].Hash()

	return snapCpy, nil
}

func (sb *Backend) snapApplyHeader(snap *Snapshot, header *types.Header) error {
	logger := sb.snapLogger(snap).New("header.number", header.Number.Uint64(), "header.hash", header.Hash().String())

	logger.Trace("BFT: apply header to voting snapshot")

	// Remove any votes on checkpoint blocks
	number := header.Number.Uint64()
	if number%snap.Epoch == 0 {
		snap.Votes = nil
		snap.Tally = make(map[common.Address]Tally)
	}

	// Resolve the authorization key and check against validators
	validator, err := sb.EngineForBlockNumber(header.Number).Author(header)
	if err != nil {
		logger.Error("BFT: invalid header author", "err", err)
		return err
	}

	logger = logger.New("header.author", validator)

	if _, v := snap.ValSet.GetByAddress(validator); v == nil {
		logger.Error("BFT: header author is not a validator")
		return istanbulcommon.ErrUnauthorized
	}

	// Read vote from header
	candidate, authorize, err := sb.EngineForBlockNumber(header.Number).ReadVote(header)
	if err != nil {
		logger.Error("BFT: invalid header vote", "err", err)
		return err
	}

	logger = logger.New("candidate", candidate.String(), "authorize", authorize)
	// Header authorized, discard any previous votes from the validator
	for i, vote := range snap.Votes {
		if vote.Validator == validator && vote.Address == candidate {
			logger.Trace("BFT: discard previous vote from tally", "old.authorize", vote.Authorize)
			// Uncast the vote from the cached tally
			snap.uncast(vote.Address, vote.Authorize)

			// Uncast the vote from the chronological list
			snap.Votes = append(snap.Votes[:i], snap.Votes[i+1:]...)
			break // only one vote allowed
		}
	}

	logger.Debug("BFT: add vote to tally")
	if snap.cast(candidate, authorize) {
		snap.Votes = append(snap.Votes, &Vote{
			Validator: validator,
			Block:     number,
			Address:   candidate,
			Authorize: authorize,
		})
	}

	// If the vote passed, update the list of validators
	if tally := snap.Tally[candidate]; tally.Votes > snap.ValSet.Size()/2 {

		if tally.Authorize {
			logger.Info("BFT: reached majority to add validator")
			snap.ValSet.AddValidator(candidate)
		} else {
			logger.Info("BFT: reached majority to remove validator")
			snap.ValSet.RemoveValidator(candidate)

			// Discard any previous votes the deauthorized validator cast
			for i := 0; i < len(snap.Votes); i++ {
				if snap.Votes[i].Validator == candidate {
					// Uncast the vote from the cached tally
					snap.uncast(snap.Votes[i].Address, snap.Votes[i].Authorize)

					// Uncast the vote from the chronological list
					snap.Votes = append(snap.Votes[:i], snap.Votes[i+1:]...)

					i--
				}
			}
		}
		// Discard any previous votes around the just changed account
		for i := 0; i < len(snap.Votes); i++ {
			if snap.Votes[i].Address == candidate {
				snap.Votes = append(snap.Votes[:i], snap.Votes[i+1:]...)
				i--
			}
		}
		delete(snap.Tally, candidate)
	}
	return nil
}
