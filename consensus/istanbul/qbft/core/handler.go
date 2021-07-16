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
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// Start implements core.Engine.Start
func (c *core) Start() error {
	c.logger.Info("QBFT: start")
	// Tests will handle events itself, so we have to make subscribeEvents()
	// be able to call in test.
	c.subscribeEvents()
	go c.handleEvents()

	// Start a new round from last sequence + 1
	c.startNewRound(common.Big0)

	return nil
}

// Stop implements core.Engine.Stop
func (c *core) Stop() error {
	c.logger.Info("QBFT: stopping...")
	c.stopTimer()
	c.unsubscribeEvents()

	// Make sure the handler goroutine exits
	c.handlerWg.Wait()
	c.logger.Info("QBFT: stopped")
	return nil
}

// ----------------------------------------------------------------------------

// Subscribe both internal and external events
func (c *core) subscribeEvents() {
	c.events = c.backend.EventMux().Subscribe(
		// external events
		istanbul.RequestEvent{},
		istanbul.MessageEvent{},
		// internal events
		backlogEvent{},
	)
	c.timeoutSub = c.backend.EventMux().Subscribe(
		timeoutEvent{},
	)
	c.finalCommittedSub = c.backend.EventMux().Subscribe(
		istanbul.FinalCommittedEvent{},
	)
}

// Unsubscribe all events
func (c *core) unsubscribeEvents() {
	c.events.Unsubscribe()
	c.timeoutSub.Unsubscribe()
	c.finalCommittedSub.Unsubscribe()
}

// handleEvents starts main qbft handler loop that processes all incoming messages
// sequentially. Each time a message is processed, internal QBFT state is mutated

// when processing a message it makes sure that the message matches the current state
// - in case the message is past, either for an older round or a state that already got acknowledge (e.g. a PREPARE message but we
// are already in Prepared state), then message is discarded
// - in case the message is future, either for a future round or a state yet to be reached (e.g. a COMMIT message but we are
// in PrePrepared state), then message is added to backlog for future processing
// - if correct time, message is handled

// Each time a message is successfully handled it is gossiped to other validators
func (c *core) handleEvents() {
	// Clear state
	defer func() {
		c.current = nil
		c.handlerWg.Done()
	}()

	c.handlerWg.Add(1)
	for {
		select {
		case event, ok := <-c.events.Chan():
			if !ok {
				return
			}

			// A real event arrived, process interesting content
			switch ev := event.Data.(type) {
			case istanbul.RequestEvent:
				// we are block proposer and look to get our block proposal validated by other validators
				r := &Request{
					Proposal: ev.Proposal,
				}
				err := c.handleRequest(r)
				if err == errFutureMessage {
					// store request for later treatment
					c.storeRequestMsg(r)
				}
			case istanbul.MessageEvent:
				// we received a message from another validator
				if err := c.handleEncodedMsg(ev.Code, ev.Payload); err != nil {
					continue
				}

				// if successfully processed, we gossip message to other validators
				c.backend.Gossip(c.valSet, ev.Code, ev.Payload)
			case backlogEvent:
				// we process again a future message that was backlogged
				// no need to check signature as it was already node when we first received message
				if err := c.handleDecodedMessage(ev.msg); err != nil {
					continue
				}

				data, err := rlp.EncodeToBytes(ev.msg)
				if err != nil {
					c.logger.Error("QBFT: can not encode backlog message", "err", err)
					continue
				}

				// if successfully processed, we gossip message to other validators
				c.backend.Gossip(c.valSet, ev.msg.Code(), data)
			}
		case _, ok := <-c.timeoutSub.Chan():
			// we received a round change timeout
			if !ok {
				return
			}
			c.handleTimeoutMsg()
		case event, ok := <-c.finalCommittedSub.Chan():
			// our block proposal got committed
			if !ok {
				return
			}
			switch event.Data.(type) {
			case istanbul.FinalCommittedEvent:
				c.handleFinalCommitted()
			}
		}
	}
}

// sendEvent sends events to mux
func (c *core) sendEvent(ev interface{}) {
	c.backend.EventMux().Post(ev)
}

func (c *core) handleEncodedMsg(code uint64, data []byte) error {
	logger := c.logger.New("code", code, "data", data)

	if _, ok := qbfttypes.MessageCodes()[code]; !ok {
		logger.Error("QBFT: invalid message event code")
		return fmt.Errorf("invalid message event code %v", code)
	}

	// Decode data into a QBFTMessage
	m, err := qbfttypes.Decode(code, data)
	if err != nil {
		logger.Error("QBFT: invalid message", "err", err)
		return err
	}

	// Verify signatures and set source address
	if err = c.verifySignatures(m); err != nil {
		return err
	}

	return c.handleDecodedMessage(m)

}

func (c *core) handleDecodedMessage(m qbfttypes.QBFTMessage) error {
	view := m.View()
	if err := c.checkMessage(m.Code(), &view); err != nil {
		// Store in the backlog it it's a future message
		if err == errFutureMessage {
			c.addToBacklog(m)
		}
		return err
	}

	return c.deliverMessage(m)
}

// Deliver to specific message handler
func (c *core) deliverMessage(m qbfttypes.QBFTMessage) error {
	var err error

	switch m.Code() {
	case qbfttypes.PreprepareCode:
		err = c.handlePreprepareMsg(m.(*qbfttypes.Preprepare))
	case qbfttypes.PrepareCode:
		err = c.handlePrepare(m.(*qbfttypes.Prepare))
	case qbfttypes.CommitCode:
		err = c.handleCommitMsg(m.(*qbfttypes.Commit))
	case qbfttypes.RoundChangeCode:
		err = c.handleRoundChange(m.(*qbfttypes.RoundChange))
	default:
		c.logger.Error("QBFT: invalid message code", "code", m.Code())
		return errInvalidMessage
	}

	return err
}

func (c *core) handleTimeoutMsg() {
	logger := c.currentLogger(true, nil)
	// Start the new round
	round := c.current.Round()
	nextRound := new(big.Int).Add(round, common.Big1)

	logger.Warn("QBFT: TIMER CHANGING ROUND", "pr", c.current.preparedRound)
	c.startNewRound(nextRound)
	logger.Warn("QBFT: TIMER CHANGED ROUND", "pr", c.current.preparedRound)

	// Send Round Change
	c.broadcastRoundChange(nextRound)
}

// Verifies the signature of the message m and of any justification payloads
// piggybacked in m, if any. It also sets the source address on the messages
// and justification payloads.
func (c *core) verifySignatures(m qbfttypes.QBFTMessage) error {
	logger := c.currentLogger(true, m)

	// Anonymous function to verify the signature of a single message or payload
	verify := func(m qbfttypes.QBFTMessage) error {
		payload, err := m.EncodePayloadForSigning()
		if err != nil {
			logger.Error("QBFT: invalid message payload", "err", err)
			return err
		}
		source, err := c.validateFn(payload, m.Signature())
		if err != nil {
			logger.Error("QBFT: invalid message signature", "err", err)
			return errInvalidSigner
		}
		m.SetSource(source)
		return nil
	}

	// Verifies the signature of the message
	if err := verify(m); err != nil {
		return err
	}

	// Verifies the signature of piggybacked justification payloads.
	switch msgType := m.(type) {
	case *qbfttypes.RoundChange:
		signedPreparePayloads := msgType.Justification
		for _, p := range signedPreparePayloads {
			if err := verify(p); err != nil {
				return err
			}
		}
	case *qbfttypes.Preprepare:
		signedRoundChangePayloads := msgType.JustificationRoundChanges
		for _, p := range signedRoundChangePayloads {
			if err := verify(p); err != nil {
				return err
			}
		}
	}

	return nil
}

func (c *core) currentLogger(state bool, msg qbfttypes.QBFTMessage) log.Logger {
	logCtx := []interface{}{
		"current.round", c.current.Round().Uint64(),
		"current.sequence", c.current.Sequence().Uint64(),
	}

	if state {
		logCtx = append(logCtx, "state", c.state)
	}

	if msg != nil {
		logCtx = append(
			logCtx,
			"msg.code", msg.Code(),
			"msg.source", msg.Source().String(),
			"msg.round", msg.View().Round.Uint64(),
			"msg.sequence", msg.View().Sequence.Uint64(),
		)
	}

	return c.logger.New(logCtx...)
}

func (c *core) withState(logger log.Logger) log.Logger {
	return logger.New("state", c.state)
}

func withMsg(logger log.Logger, msg qbfttypes.QBFTMessage) log.Logger {
	return logger.New(
		"msg.code", msg.Code(),
		"msg.source", msg.Source().String(),
		"msg.round", msg.View().Round.Uint64(),
		"msg.sequence", msg.View().Sequence.Uint64(),
	)
}
