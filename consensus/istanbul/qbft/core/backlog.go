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
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
)

var (
	// msgPriority is defined for calculating processing priority to speedup consensus
	// msgPreprepare > msgCommit > msgPrepare
	msgPriority = map[uint64]int{
		qbfttypes.PreprepareCode: 1,
		qbfttypes.CommitCode:     2,
		qbfttypes.PrepareCode:    3,
	}
)

// checkMessage checks that a message matches our current QBFT state
//
// In particular it ensures that
// - message has the expected round
// - message has the expected sequence
// - message type is expected given our current state

// return errInvalidMessage if the message is invalid
// return errFutureMessage if the message view is larger than current view
// return errOldMessage if the message view is smaller than current view
func (c *core) checkMessage(msgCode uint64, view *istanbul.View) error {
	if view == nil || view.Sequence == nil || view.Round == nil {
		return errInvalidMessage
	}

	if msgCode == qbfttypes.RoundChangeCode {
		// if ROUND-CHANGE message
		// check that
		// - sequence matches our current sequence
		// - round is in the future
		if view.Sequence.Cmp(c.currentView().Sequence) > 0 {
			return errFutureMessage
		} else if view.Cmp(c.currentView()) < 0 {
			return errOldMessage
		}
		return nil
	}

	// If not ROUND-CHANGE
	// check that round and sequence equals our current round and sequence
	if view.Cmp(c.currentView()) > 0 {
		return errFutureMessage
	}

	if view.Cmp(c.currentView()) < 0 {
		return errOldMessage
	}

	switch c.state {
	case StateAcceptRequest:
		// StateAcceptRequest only accepts msgPreprepare and msgRoundChange
		// other messages are future messages
		if msgCode > qbfttypes.PreprepareCode {
			return errFutureMessage
		}
		return nil
	case StatePreprepared:
		// StatePreprepared only accepts msgPrepare and msgRoundChange
		// message less than msgPrepare are invalid and greater are future messages
		if msgCode < qbfttypes.PrepareCode {
			return errInvalidMessage
		} else if msgCode > qbfttypes.PrepareCode {
			return errFutureMessage
		}
		return nil
	case StatePrepared:
		// StatePrepared only accepts msgCommit and msgRoundChange
		// other messages are invalid messages
		if msgCode < qbfttypes.CommitCode {
			return errInvalidMessage
		}
		return nil
	case StateCommitted:
		// StateCommit rejects all messages other than msgRoundChange
		return errInvalidMessage
	}
	return nil
}

// addToBacklog allows to postpone the processing of future messages

// it adds the message to backlog which is read on every state change
func (c *core) addToBacklog(msg qbfttypes.QBFTMessage) {
	logger := c.currentLogger(true, msg)

	src := msg.Source()
	if src == c.Address() {
		logger.Warn("QBFT: backlog from self")
		return
	}

	logger.Trace("QBFT: new backlog message", "backlogs_size", len(c.backlogs))

	c.backlogsMu.Lock()
	defer c.backlogsMu.Unlock()

	backlog := c.backlogs[src]
	if backlog == nil {
		backlog = prque.New()
		c.backlogs[src] = backlog
	}
	view := msg.View()
	backlog.Push(msg, toPriority(msg.Code(), &view))
}

// processBacklog lookup for future messages that have been backlogged and post it on
// the event channel so main handler loop can handle it

// It is called on every state change
func (c *core) processBacklog() {
	c.backlogsMu.Lock()
	defer c.backlogsMu.Unlock()

	for srcAddress, backlog := range c.backlogs {
		if backlog == nil {
			continue
		}
		_, src := c.valSet.GetByAddress(srcAddress)
		if src == nil {
			// validator is not available
			delete(c.backlogs, srcAddress)
			continue
		}
		logger := c.logger.New("from", src, "state", c.state)
		isFuture := false

		logger.Trace("QBFT: process backlog")

		// We stop processing if
		//   1. backlog is empty
		//   2. The first message in queue is a future message
		for !(backlog.Empty() || isFuture) {
			m, prio := backlog.Pop()

			var code uint64
			var view istanbul.View
			var event backlogEvent

			msg := m.(qbfttypes.QBFTMessage)
			code = msg.Code()
			view = msg.View()
			event.msg = msg

			// Push back if it's a future message
			err := c.checkMessage(code, &view)
			if err != nil {
				if err == errFutureMessage {
					// this is still a future message
					logger.Trace("QBFT: stop processing backlog", "msg", m)
					backlog.Push(m, prio)
					isFuture = true
					break
				}
				logger.Trace("QBFT: skip backlog message", "msg", m, "err", err)
				continue
			}
			logger.Trace("QBFT: post backlog event", "msg", m)

			event.src = src
			go c.sendEvent(event)
		}
	}
}

func toPriority(msgCode uint64, view *istanbul.View) float32 {
	if msgCode == qbfttypes.RoundChangeCode {
		// For msgRoundChange, set the message priority based on its sequence
		return -float32(view.Sequence.Uint64() * 1000)
	}
	// FIXME: round will be reset as 0 while new sequence
	// 10 * Round limits the range of message code is from 0 to 9
	// 1000 * Sequence limits the range of round is from 0 to 99
	return -float32(view.Sequence.Uint64()*1000 + view.Round.Uint64()*10 + uint64(msgPriority[msgCode]))
}
