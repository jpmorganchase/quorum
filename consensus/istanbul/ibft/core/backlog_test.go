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
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	ibfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/ibft/types"
	"github.com/ethereum/go-ethereum/event"
	"github.com/ethereum/go-ethereum/log"
	"gopkg.in/karalabe/cookiejar.v2/collections/prque"
)

func TestCheckMessage(t *testing.T) {
	c := &core{
		state: ibfttypes.StateAcceptRequest,
		current: newRoundState(&istanbul.View{
			Sequence: big.NewInt(1),
			Round:    big.NewInt(0),
		}, newTestValidatorSet(4), common.Hash{}, nil, nil, nil),
	}

	// invalid view format
	err := c.checkMessage(ibfttypes.MsgPreprepare, nil)
	if err != istanbulcommon.ErrInvalidMessage {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidMessage)
	}

	testStates := []ibfttypes.State{ibfttypes.StateAcceptRequest, ibfttypes.StatePreprepared, ibfttypes.StatePrepared, ibfttypes.StateCommitted}
	testCode := []uint64{ibfttypes.MsgPreprepare, ibfttypes.MsgPrepare, ibfttypes.MsgCommit, ibfttypes.MsgRoundChange}

	// future sequence
	v := &istanbul.View{
		Sequence: big.NewInt(2),
		Round:    big.NewInt(0),
	}
	for i := 0; i < len(testStates); i++ {
		c.state = testStates[i]
		for j := 0; j < len(testCode); j++ {
			err := c.checkMessage(testCode[j], v)
			if err != istanbulcommon.ErrFutureMessage {
				t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrFutureMessage)
			}
		}
	}

	// future round
	v = &istanbul.View{
		Sequence: big.NewInt(1),
		Round:    big.NewInt(1),
	}
	for i := 0; i < len(testStates); i++ {
		c.state = testStates[i]
		for j := 0; j < len(testCode); j++ {
			err := c.checkMessage(testCode[j], v)
			if testCode[j] == ibfttypes.MsgRoundChange {
				if err != nil {
					t.Errorf("error mismatch: have %v, want nil", err)
				}
			} else if err != istanbulcommon.ErrFutureMessage {
				t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrFutureMessage)
			}
		}
	}

	// current view but waiting for round change
	v = &istanbul.View{
		Sequence: big.NewInt(1),
		Round:    big.NewInt(0),
	}
	c.waitingForRoundChange = true
	for i := 0; i < len(testStates); i++ {
		c.state = testStates[i]
		for j := 0; j < len(testCode); j++ {
			err := c.checkMessage(testCode[j], v)
			if testCode[j] == ibfttypes.MsgRoundChange {
				if err != nil {
					t.Errorf("error mismatch: have %v, want nil", err)
				}
			} else if err != istanbulcommon.ErrFutureMessage {
				t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrFutureMessage)
			}
		}
	}
	c.waitingForRoundChange = false

	v = c.currentView()
	// current view, state = ibfttypes.StateAcceptRequest
	c.state = ibfttypes.StateAcceptRequest
	for i := 0; i < len(testCode); i++ {
		err = c.checkMessage(testCode[i], v)
		if testCode[i] == ibfttypes.MsgRoundChange {
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
		} else if testCode[i] == ibfttypes.MsgPreprepare {
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
		} else {
			if err != istanbulcommon.ErrFutureMessage {
				t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrFutureMessage)
			}
		}
	}

	// current view, state = StatePreprepared
	c.state = ibfttypes.StatePreprepared
	for i := 0; i < len(testCode); i++ {
		err = c.checkMessage(testCode[i], v)
		if testCode[i] == ibfttypes.MsgRoundChange {
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
		} else if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}
	}

	// current view, state = ibfttypes.StatePrepared
	c.state = ibfttypes.StatePrepared
	for i := 0; i < len(testCode); i++ {
		err = c.checkMessage(testCode[i], v)
		if testCode[i] == ibfttypes.MsgRoundChange {
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
		} else if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}
	}

	// current view, state = ibfttypes.StateCommitted
	c.state = ibfttypes.StateCommitted
	for i := 0; i < len(testCode); i++ {
		err = c.checkMessage(testCode[i], v)
		if testCode[i] == ibfttypes.MsgRoundChange {
			if err != nil {
				t.Errorf("error mismatch: have %v, want nil", err)
			}
		} else if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}
	}

}

func TestStoreBacklog(t *testing.T) {
	c := &core{
		logger:     log.New("backend", "test", "id", 0),
		valSet:     newTestValidatorSet(1),
		backlogs:   make(map[common.Address]*prque.Prque),
		backlogsMu: new(sync.Mutex),
	}
	v := &istanbul.View{
		Round:    big.NewInt(10),
		Sequence: big.NewInt(10),
	}
	p := c.valSet.GetByIndex(0)
	// push preprepare msg
	preprepare := &istanbul.Preprepare{
		View:     v,
		Proposal: makeBlock(1),
	}
	prepreparePayload, _ := ibfttypes.Encode(preprepare)
	m := &ibfttypes.Message{
		Code: ibfttypes.MsgPreprepare,
		Msg:  prepreparePayload,
	}
	c.storeBacklog(m, p)
	msg := c.backlogs[p.Address()].PopItem()
	if !reflect.DeepEqual(msg, m) {
		t.Errorf("message mismatch: have %v, want %v", msg, m)
	}

	// push prepare msg
	subject := &istanbul.Subject{
		View:   v,
		Digest: common.StringToHash("1234567890"),
	}
	subjectPayload, _ := ibfttypes.Encode(subject)

	m = &ibfttypes.Message{
		Code: ibfttypes.MsgPrepare,
		Msg:  subjectPayload,
	}
	c.storeBacklog(m, p)
	msg = c.backlogs[p.Address()].PopItem()
	if !reflect.DeepEqual(msg, m) {
		t.Errorf("message mismatch: have %v, want %v", msg, m)
	}

	// push commit msg
	m = &ibfttypes.Message{
		Code: ibfttypes.MsgCommit,
		Msg:  subjectPayload,
	}
	c.storeBacklog(m, p)
	msg = c.backlogs[p.Address()].PopItem()
	if !reflect.DeepEqual(msg, m) {
		t.Errorf("message mismatch: have %v, want %v", msg, m)
	}

	// push roundChange msg
	m = &ibfttypes.Message{
		Code: ibfttypes.MsgRoundChange,
		Msg:  subjectPayload,
	}
	c.storeBacklog(m, p)
	msg = c.backlogs[p.Address()].PopItem()
	if !reflect.DeepEqual(msg, m) {
		t.Errorf("message mismatch: have %v, want %v", msg, m)
	}
}

func TestProcessFutureBacklog(t *testing.T) {
	backend := &testSystemBackend{
		events: new(event.TypeMux),
	}
	c := &core{
		logger:     log.New("backend", "test", "id", 0),
		valSet:     newTestValidatorSet(1),
		backlogs:   make(map[common.Address]*prque.Prque),
		backlogsMu: new(sync.Mutex),
		backend:    backend,
		current: newRoundState(&istanbul.View{
			Sequence: big.NewInt(1),
			Round:    big.NewInt(0),
		}, newTestValidatorSet(4), common.Hash{}, nil, nil, nil),
		state: ibfttypes.StateAcceptRequest,
	}
	c.subscribeEvents()
	defer c.unsubscribeEvents()

	v := &istanbul.View{
		Round:    big.NewInt(10),
		Sequence: big.NewInt(10),
	}
	p := c.valSet.GetByIndex(0)
	// push a future msg
	subject := &istanbul.Subject{
		View:   v,
		Digest: common.StringToHash("1234567890"),
	}
	subjectPayload, _ := ibfttypes.Encode(subject)
	m := &ibfttypes.Message{
		Code: ibfttypes.MsgCommit,
		Msg:  subjectPayload,
	}
	c.storeBacklog(m, p)
	c.processBacklog()

	const timeoutDura = 2 * time.Second
	timeout := time.NewTimer(timeoutDura)
	select {
	case e, ok := <-c.events.Chan():
		if !ok {
			return
		}
		t.Errorf("unexpected events comes: %v", e)
	case <-timeout.C:
		// success
	}
}

func TestProcessBacklog(t *testing.T) {
	v := &istanbul.View{
		Round:    big.NewInt(0),
		Sequence: big.NewInt(1),
	}
	preprepare := &istanbul.Preprepare{
		View:     v,
		Proposal: makeBlock(1),
	}
	prepreparePayload, _ := ibfttypes.Encode(preprepare)

	subject := &istanbul.Subject{
		View:   v,
		Digest: common.StringToHash("1234567890"),
	}
	subjectPayload, _ := ibfttypes.Encode(subject)

	msgs := []*ibfttypes.Message{
		{
			Code: ibfttypes.MsgPreprepare,
			Msg:  prepreparePayload,
		},
		{
			Code: ibfttypes.MsgPrepare,
			Msg:  subjectPayload,
		},
		{
			Code: ibfttypes.MsgCommit,
			Msg:  subjectPayload,
		},
		{
			Code: ibfttypes.MsgRoundChange,
			Msg:  subjectPayload,
		},
	}
	for i := 0; i < len(msgs); i++ {
		testProcessBacklog(t, msgs[i])
	}
}

func testProcessBacklog(t *testing.T, msg *ibfttypes.Message) {
	vset := newTestValidatorSet(1)
	backend := &testSystemBackend{
		events: new(event.TypeMux),
		peers:  vset,
	}
	c := &core{
		logger:     log.New("backend", "test", "id", 0),
		backlogs:   make(map[common.Address]*prque.Prque),
		backlogsMu: new(sync.Mutex),
		valSet:     vset,
		backend:    backend,
		state:      ibfttypes.State(msg.Code),
		current: newRoundState(&istanbul.View{
			Sequence: big.NewInt(1),
			Round:    big.NewInt(0),
		}, newTestValidatorSet(4), common.Hash{}, nil, nil, nil),
	}
	c.subscribeEvents()
	defer c.unsubscribeEvents()

	c.storeBacklog(msg, vset.GetByIndex(0))
	c.processBacklog()

	const timeoutDura = 2 * time.Second
	timeout := time.NewTimer(timeoutDura)
	select {
	case ev := <-c.events.Chan():
		e, ok := ev.Data.(backlogEvent)
		if !ok {
			t.Errorf("unexpected event comes: %v", reflect.TypeOf(ev.Data))
		}
		if e.msg.Code != msg.Code {
			t.Errorf("message code mismatch: have %v, want %v", e.msg.Code, msg.Code)
		}
		// success
	case <-timeout.C:
		t.Error("unexpected timeout occurs")
	}
}
