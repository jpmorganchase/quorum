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
	"time"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/consensus"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"github.com/ethereum/go-ethereum/rlp"
)

// sendPreprepareMsg is called either
// - when we are proposer after calling `miner.Seal(...)`
// - roundChange happens and we are the proposer

// It
// - creates and sign PRE-PREPARE message with block proposed on `miner.Seal()`
// - extends PRE-PREPARE message with ROUND-CHANGE and PREPARE justification
// - broadcast PRE-PREPARE message to other validators
func (c *core) sendPreprepareMsg(request *Request) {
	logger := c.currentLogger(true, nil)

	// If I'm the proposer and I have the same sequence with the proposal
	if c.current.Sequence().Cmp(request.Proposal.Number()) == 0 && c.IsProposer() {
		// Creates PRE-PREPARE message
		curView := c.currentView()
		preprepare := qbfttypes.NewPreprepare(curView.Sequence, curView.Round, request.Proposal)
		preprepare.SetSource(c.Address())

		// Sign payload
		encodedPayload, err := preprepare.EncodePayloadForSigning()
		if err != nil {
			withMsg(logger, preprepare).Error("QBFT: failed to encode payload of PRE-PREPARE message", "err", err)
			return
		}
		signature, err := c.backend.Sign(encodedPayload)
		if err != nil {
			withMsg(logger, preprepare).Error("QBFT: failed to sign PRE-PREPARE message", "err", err)
			return
		}
		preprepare.SetSignature(signature)

		// Extend PRE-PREPARE message with ROUND-CHANGE justification
		if request.RCMessages != nil {
			preprepare.JustificationRoundChanges = make([]*qbfttypes.SignedRoundChangePayload, 0)
			for _, m := range request.RCMessages.Values() {
				preprepare.JustificationRoundChanges = append(preprepare.JustificationRoundChanges, &m.(*qbfttypes.RoundChange).SignedRoundChangePayload)
				withMsg(logger, preprepare).Trace("QBFT: add ROUND-CHANGE justification", "rc", m.(*qbfttypes.RoundChange).SignedRoundChangePayload)
			}
			withMsg(logger, preprepare).Trace("QBFT: extended PRE-PREPARE message with ROUND-CHANGE justifications", "justifications", preprepare.JustificationRoundChanges)
		}

		// Extend PRE-PREPARE message with PREPARE justification
		if request.PrepareMessages != nil {
			preprepare.JustificationPrepares = request.PrepareMessages
			withMsg(logger, preprepare).Trace("QBFT: extended PRE-PREPARE message with PREPARE justification", "justification", preprepare.JustificationPrepares)
		}

		// RLP-encode message
		payload, err := rlp.EncodeToBytes(&preprepare)
		if err != nil {
			withMsg(logger, preprepare).Error("QBFT: failed to encode PRE-PREPARE message", "err", err)
			return
		}

		logger = withMsg(logger, preprepare).New("block.number", preprepare.Proposal.Number().Uint64(), "block.hash", preprepare.Proposal.Hash().String())

		logger.Info("QBFT: broadcast PRE-PREPARE message", "payload", hexutil.Encode(payload))

		// Broadcast RLP-encoded message
		if err = c.backend.Broadcast(c.valSet, preprepare.Code(), payload); err != nil {
			logger.Error("QBFT: failed to broadcast PRE-PREPARE message", "err", err)
			return
		}

		// Set the preprepareSent to the current round
		c.current.preprepareSent = curView.Round
	}
}

// handlePreprepareMsg is called when receiving a PRE-PREPARE message from the proposer

// It
// - validates PRE-PREPARE message was created by the right proposer node
// - validates PRE-PREPARE message justification
// - validates PRE-PREPARE message block proposal
func (c *core) handlePreprepareMsg(preprepare *qbfttypes.Preprepare) error {
	logger := c.currentLogger(true, preprepare)

	logger = logger.New("proposal.number", preprepare.Proposal.Number().Uint64(), "proposal.hash", preprepare.Proposal.Hash().String())

	c.logger.Info("QBFT: handle PRE-PREPARE message")

	// Validates PRE-PREPARE message comes from current proposer
	if !c.valSet.IsProposer(preprepare.Source()) {
		logger.Warn("QBFT: ignore PRE-PREPARE message from non proposer", "proposer", c.valSet.GetProposer().Address())
		return errNotFromProposer
	}

	// Validates PRE-PREPARE message justification
	if preprepare.Round.Uint64() > 0 {
		if err := isJustified(preprepare.Proposal, preprepare.JustificationRoundChanges, preprepare.JustificationPrepares, c.QuorumSize()); err != nil {
			logger.Warn("QBFT: invalid PRE-PREPARE message justification", "err", err)
			return errInvalidPreparedBlock
		}
	}

	// Validates PRE-PREPARE block proposal we received
	if duration, err := c.backend.Verify(preprepare.Proposal); err != nil {
		// if it's a future block, we will handle it again after the duration
		if err == consensus.ErrFutureBlock {
			logger.Info("QBFT: PRE-PREPARE block proposal is in the future (will be treated again later)", "duration", duration)

			// start a timer to re-input PRE-PREPARE message as a backlog event
			c.stopFuturePreprepareTimer()
			c.futurePreprepareTimer = time.AfterFunc(duration, func() {
				_, validator := c.valSet.GetByAddress(preprepare.Source())
				c.sendEvent(backlogEvent{
					src: validator,
					msg: preprepare,
				})
			})
		}

		logger.Warn("QBFT: invalid PRE-PREPARE block proposal", "err", err)

		return err
	}

	// Here is about to accept the PRE-PREPARE
	if c.state == StateAcceptRequest {
		c.logger.Info("QBFT: accepted PRE-PREPARE message")

		// Re-initialize ROUND-CHANGE timer
		c.newRoundChangeTimer()
		c.consensusTimestamp = time.Now()

		// Update current state
		c.current.SetPreprepare(preprepare)
		c.setState(StatePreprepared)

		// Broadcast prepare message to other validators
		c.broadcastPrepare()
	}

	return nil
}
