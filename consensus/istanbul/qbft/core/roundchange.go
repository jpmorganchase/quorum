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

package core

import (
	"math/big"
	"sort"
	"sync"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// broadcastNextRoundChange sends the ROUND CHANGE message with current round + 1
func (c *core) broadcastNextRoundChange() {
	cv := c.currentView()
	c.broadcastRoundChange(new(big.Int).Add(cv.Round, common.Big1))
}

// broadcastRoundChange is called when either
// - ROUND-CHANGE timeout expires (meaning either we have not received PRE-PREPARE message or we have not received a quorum of COMMIT messages)
// -

// It
// - Creates and sign ROUND-CHANGE message
// - broadcast the ROUND-CHANGE message with the given round
func (c *core) broadcastRoundChange(round *big.Int) {
	logger := c.currentLogger(true, nil)

	// Validates new round corresponds to current view
	cv := c.currentView()
	if cv.Round.Cmp(round) > 0 {
		logger.Error("QBFT: invalid past target round", "target", round)
		return
	}

	roundChange := qbfttypes.NewRoundChange(c.current.Sequence(), round, c.current.preparedRound, c.current.preparedBlock)

	// Sign message
	encodedPayload, err := roundChange.EncodePayloadForSigning()
	if err != nil {
		withMsg(logger, roundChange).Error("QBFT: failed to encode ROUND-CHANGE message", "err", err)
		return
	}
	signature, err := c.backend.Sign(encodedPayload)
	if err != nil {
		withMsg(logger, roundChange).Error("QBFT: failed to sign ROUND-CHANGE message", "err", err)
		return
	}
	roundChange.SetSignature(signature)

	// Extend ROUND-CHANGE message with PREPARE justification
	if c.QBFTPreparedPrepares != nil {
		roundChange.Justification = c.QBFTPreparedPrepares
		withMsg(logger, roundChange).Debug("QBFT: extended ROUND-CHANGE message with PREPARE justification", "justification", roundChange.Justification)
	}

	// RLP-encode message
	data, err := rlp.EncodeToBytes(roundChange)
	if err != nil {
		withMsg(logger, roundChange).Error("QBFT: failed to encode ROUND-CHANGE message", "err", err)
		return
	}

	withMsg(logger, roundChange).Info("QBFT: broadcast ROUND-CHANGE message", "payload", hexutil.Encode(data))

	// Broadcast RLP-encoded message
	if err = c.backend.Broadcast(c.valSet, roundChange.Code(), data); err != nil {
		withMsg(logger, roundChange).Error("QBFT: failed to broadcast ROUND-CHANGE message", "err", err)
		return
	}
}

// handleRoundChange is called when receiving a ROUND-CHANGE message from another validator
// - accumulates ROUND-CHANGE messages until reaching quorum for a given round
// - when quorum of ROUND-CHANGE messages is reached then
func (c *core) handleRoundChange(roundChange *qbfttypes.RoundChange) error {
	logger := c.currentLogger(true, roundChange)

	view := roundChange.View()
	currentRound := c.currentView().Round

	// number of validators we received ROUND-CHANGE from for a round higher than the current one
	num := c.roundChangeSet.higherRoundMessages(currentRound)

	// number of validators we received ROUND-CHANGE from for the current round
	currentRoundMessages := c.roundChangeSet.getRCMessagesForGivenRound(currentRound)

	logger.Info("QBFT: handle ROUND-CHANGE message", "higherRoundChanges.count", num, "currentRoundChanges.count", currentRoundMessages)

	// Add ROUND-CHANGE message to message set
	if view.Round.Cmp(currentRound) >= 0 {
		var prepareMessages []*qbfttypes.Prepare = nil
		var pr *big.Int = nil
		var pb *types.Block = nil
		if roundChange.PreparedRound != nil && roundChange.PreparedBlock != nil && roundChange.Justification != nil && len(roundChange.Justification) > 0 {
			prepareMessages = roundChange.Justification
			pr = roundChange.PreparedRound
			pb = roundChange.PreparedBlock
		}
		err := c.roundChangeSet.Add(view.Round, roundChange, pr, pb, prepareMessages, c.QuorumSize())
		if err != nil {
			logger.Warn("QBFT: failed to add ROUND-CHANGE message", "err", err)
			return err
		}
	}

	// number of validators we received ROUND-CHANGE from for a round higher than the current one
	num = c.roundChangeSet.higherRoundMessages(currentRound)

	// number of validators we received ROUND-CHANGE from for the current round
	currentRoundMessages = c.roundChangeSet.getRCMessagesForGivenRound(currentRound)

	logger = logger.New("higherRoundChanges.count", num, "currentRoundChanges.count", currentRoundMessages)

	if num == c.valSet.F()+1 {
		// We received F+1 ROUND-CHANGE messages (this may happen before our timeout exprired)
		// we start new round and broadcast ROUND-CHANGE message
		newRound := c.roundChangeSet.getMinRoundChange(currentRound)

		logger.Info("QBFT: received F+1 ROUND-CHANGE messages", "F", c.valSet.F())

		c.startNewRound(newRound)
		c.broadcastRoundChange(newRound)
	} else if currentRoundMessages >= c.QuorumSize() && c.IsProposer() && c.current.preprepareSent.Cmp(currentRound) < 0 {
		logger.Info("QBFT: received quorum of ROUND-CHANGE messages")

		// We received quorum of ROUND-CHANGE for current round and we are proposer

		// If we have received a quorum of PREPARE message
		// then we propose the same block proposal again if not we
		// propose the block proposal that we generated
		_, proposal := c.highestPrepared(currentRound)
		if proposal == nil {
			proposal = c.current.pendingRequest.Proposal
		}

		// Prepare justification for ROUND-CHANGE messages
		roundChangeMessages := c.roundChangeSet.roundChanges[currentRound.Uint64()]
		rcSignedPayloads := make([]*qbfttypes.SignedRoundChangePayload, 0)
		for _, m := range roundChangeMessages.Values() {
			rcMsg := m.(*qbfttypes.RoundChange)
			rcSignedPayloads = append(rcSignedPayloads, &rcMsg.SignedRoundChangePayload)
		}

		prepareMessages := c.roundChangeSet.prepareMessages[currentRound.Uint64()]
		if err := isJustified(proposal, rcSignedPayloads, prepareMessages, c.QuorumSize()); err != nil {
			logger.Error("QBFT: invalid ROUND-CHANGE message justification", "err", err)
			return nil
		}

		r := &Request{
			Proposal:        proposal,
			RCMessages:      roundChangeMessages,
			PrepareMessages: prepareMessages,
		}
		c.sendPreprepareMsg(r)
	} else {
		logger.Debug("QBFT: accepted ROUND-CHANGE messages")
	}
	return nil
}

// highestPrepared returns the highest Prepared Round and the corresponding Prepared Block
func (c *core) highestPrepared(round *big.Int) (*big.Int, istanbul.Proposal) {
	return c.roundChangeSet.highestPreparedRound[round.Uint64()], c.roundChangeSet.highestPreparedBlock[round.Uint64()]
}

// ----------------------------------------------------------------------------

func newRoundChangeSet(valSet istanbul.ValidatorSet) *roundChangeSet {
	return &roundChangeSet{
		validatorSet:         valSet,
		roundChanges:         make(map[uint64]*qbftMsgSet),
		prepareMessages:      make(map[uint64][]*qbfttypes.Prepare),
		highestPreparedRound: make(map[uint64]*big.Int),
		highestPreparedBlock: make(map[uint64]istanbul.Proposal),
		mu:                   new(sync.Mutex),
	}
}

type roundChangeSet struct {
	validatorSet         istanbul.ValidatorSet
	roundChanges         map[uint64]*qbftMsgSet
	prepareMessages      map[uint64][]*qbfttypes.Prepare
	highestPreparedRound map[uint64]*big.Int
	highestPreparedBlock map[uint64]istanbul.Proposal
	mu                   *sync.Mutex
}

func (rcs *roundChangeSet) NewRound(r *big.Int) {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()
	round := r.Uint64()
	if rcs.roundChanges[round] == nil {
		rcs.roundChanges[round] = newQBFTMsgSet(rcs.validatorSet)
	}
	if rcs.prepareMessages[round] == nil {
		rcs.prepareMessages[round] = make([]*qbfttypes.Prepare, 0)
	}
}

// Add adds the round and message into round change set
func (rcs *roundChangeSet) Add(r *big.Int, msg qbfttypes.QBFTMessage, preparedRound *big.Int, preparedBlock istanbul.Proposal, prepareMessages []*qbfttypes.Prepare, quorumSize int) error {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	round := r.Uint64()
	if rcs.roundChanges[round] == nil {
		rcs.roundChanges[round] = newQBFTMsgSet(rcs.validatorSet)
	}
	if err := rcs.roundChanges[round].Add(msg); err != nil {
		return err
	}

	if preparedRound != nil && (rcs.highestPreparedRound[round] == nil || preparedRound.Cmp(rcs.highestPreparedRound[round]) > 0) {
		roundChange := msg.(*qbfttypes.RoundChange)
		if hasMatchingRoundChangeAndPrepares(roundChange, prepareMessages, quorumSize) == nil {
			rcs.highestPreparedRound[round] = preparedRound
			rcs.highestPreparedBlock[round] = preparedBlock
			rcs.prepareMessages[round] = prepareMessages
		}
	}

	return nil
}

// higherRoundMessages returns the count of validators we received a ROUND-CHANGE message from
// for any round greater than the given round
func (rcs *roundChangeSet) higherRoundMessages(round *big.Int) int {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	addresses := make(map[common.Address]struct{})
	for k, rms := range rcs.roundChanges {
		if k > round.Uint64() {
			for addr := range rms.messages {
				addresses[addr] = struct{}{}
			}
		}
	}
	return len(addresses)
}

// getRCMessagesForGivenRound return the count ROUND-CHANGE messages
// received for a given round
func (rcs *roundChangeSet) getRCMessagesForGivenRound(round *big.Int) int {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	if rms := rcs.roundChanges[round.Uint64()]; rms != nil {
		return len(rms.messages)
	}
	return 0
}

// getMinRoundChange returns the minimum round greater than the given round
func (rcs *roundChangeSet) getMinRoundChange(round *big.Int) *big.Int {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	var keys []int
	for k := range rcs.roundChanges {
		if k > round.Uint64() {
			keys = append(keys, int(k))
		}
	}
	sort.Ints(keys)
	if len(keys) == 0 {
		return round
	}
	return big.NewInt(int64(keys[0]))
}

// ClearLowerThan deletes the messages for round earlier than the given round
func (rcs *roundChangeSet) ClearLowerThan(round *big.Int) {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	for k, rms := range rcs.roundChanges {
		if len(rms.Values()) == 0 || k < round.Uint64() {
			delete(rcs.roundChanges, k)
			delete(rcs.highestPreparedRound, k)
			delete(rcs.highestPreparedBlock, k)
			delete(rcs.prepareMessages, k)
		}
	}
}

// MaxRound returns the max round which the number of messages is equal or larger than num
func (rcs *roundChangeSet) MaxRound(num int) *big.Int {
	rcs.mu.Lock()
	defer rcs.mu.Unlock()

	var maxRound *big.Int
	for k, rms := range rcs.roundChanges {
		if rms.Size() < num {
			continue
		}
		r := big.NewInt(int64(k))
		if maxRound == nil || maxRound.Cmp(r) < 0 {
			maxRound = r
		}
	}
	return maxRound
}
