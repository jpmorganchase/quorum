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
	"math"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	ibfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/ibft/types"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestHandlePrepare(t *testing.T) {
	N := uint64(4)
	F := uint64(1)

	proposal := newTestProposal()
	expectedSubject := &istanbul.Subject{
		View: &istanbul.View{
			Round:    big.NewInt(0),
			Sequence: proposal.Number(),
		},
		Digest: proposal.Hash(),
	}

	testCases := []struct {
		system      *testSystem
		expectedErr error
	}{
		{
			// normal case
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine
					c.valSet = backend.peers
					c.current = newTestRoundState(
						&istanbul.View{
							Round:    big.NewInt(0),
							Sequence: big.NewInt(1),
						},
						c.valSet,
					)

					if i == 0 {
						// replica 0 is the proposer
						c.state = ibfttypes.StatePreprepared
					}
				}
				return sys
			}(),
			nil,
		},
		{
			// future ibfttypes.Message
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine
					c.valSet = backend.peers
					if i == 0 {
						// replica 0 is the proposer
						c.current = newTestRoundState(
							expectedSubject.View,
							c.valSet,
						)
						c.state = ibfttypes.StatePreprepared
					} else {
						c.current = newTestRoundState(
							&istanbul.View{
								Round:    big.NewInt(2),
								Sequence: big.NewInt(3),
							},
							c.valSet,
						)
					}
				}
				return sys
			}(),
			istanbulcommon.ErrFutureMessage,
		},
		{
			// subject not match
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine
					c.valSet = backend.peers
					if i == 0 {
						// replica 0 is the proposer
						c.current = newTestRoundState(
							expectedSubject.View,
							c.valSet,
						)
						c.state = ibfttypes.StatePreprepared
					} else {
						c.current = newTestRoundState(
							&istanbul.View{
								Round:    big.NewInt(0),
								Sequence: big.NewInt(0),
							},
							c.valSet,
						)
					}
				}
				return sys
			}(),
			istanbulcommon.ErrOldMessage,
		},
		{
			// subject not match
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				for i, backend := range sys.backends {
					c := backend.engine
					c.valSet = backend.peers
					if i == 0 {
						// replica 0 is the proposer
						c.current = newTestRoundState(
							expectedSubject.View,
							c.valSet,
						)
						c.state = ibfttypes.StatePreprepared
					} else {
						c.current = newTestRoundState(
							&istanbul.View{
								Round:    big.NewInt(0),
								Sequence: big.NewInt(1)},
							c.valSet,
						)
					}
				}
				return sys
			}(),
			istanbulcommon.ErrInconsistentSubject,
		},
		{
			func() *testSystem {
				sys := NewTestSystemWithBackend(N, F)

				// save less than Ceil(2*N/3) replica
				sys.backends = sys.backends[int(math.Ceil(float64(2*N)/3)):]

				for i, backend := range sys.backends {
					c := backend.engine
					c.valSet = backend.peers
					c.current = newTestRoundState(
						expectedSubject.View,
						c.valSet,
					)

					if i == 0 {
						// replica 0 is the proposer
						c.state = ibfttypes.StatePreprepared
					}
				}
				return sys
			}(),
			nil,
		},
		// TODO: double send ibfttypes.Message
	}

OUTER:
	for _, test := range testCases {
		test.system.Run(false)

		v0 := test.system.backends[0]
		r0 := v0.engine

		for i, v := range test.system.backends {
			validator := r0.valSet.GetByIndex(uint64(i))
			m, _ := ibfttypes.Encode(v.engine.current.Subject())
			if err := r0.handlePrepare(&ibfttypes.Message{
				Code:    ibfttypes.MsgPrepare,
				Msg:     m,
				Address: validator.Address(),
			}, validator); err != nil {
				if err != test.expectedErr {
					t.Errorf("error mismatch: have %v, want %v", err, test.expectedErr)
				}
				if r0.current.IsHashLocked() {
					t.Errorf("block should not be locked")
				}
				continue OUTER
			}
		}

		// prepared is normal case
		if r0.state != ibfttypes.StatePrepared {
			// There are not enough PREPARE messages in core
			if r0.state != ibfttypes.StatePreprepared {
				t.Errorf("state mismatch: have %v, want %v", r0.state, ibfttypes.StatePreprepared)
			}
			if r0.current.Prepares.Size() >= r0.QuorumSize() {
				t.Errorf("the size of PREPARE messages should be less than %v", r0.QuorumSize())
			}
			if r0.current.IsHashLocked() {
				t.Errorf("block should not be locked")
			}

			continue
		}

		// core should have 2F+1 before Ceil2Nby3Block and Ceil(2N/3) after Ceil2Nby3Block PREPARE messages
		if r0.current.Prepares.Size() < r0.QuorumSize() {
			t.Errorf("the size of PREPARE messages should be larger than 2F+1 or ceil(2N/3): size %v", r0.current.Commits.Size())
		}

		// a ibfttypes.Message will be delivered to backend if ceil(2N/3)
		if int64(len(v0.sentMsgs)) != 1 {
			t.Errorf("the Send() should be called once: times %v", len(test.system.backends[0].sentMsgs))
		}

		// verify COMMIT messages
		decodedMsg := new(ibfttypes.Message)
		err := decodedMsg.FromPayload(v0.sentMsgs[0], nil)
		if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}

		if decodedMsg.Code != ibfttypes.MsgCommit {
			t.Errorf("ibfttypes.Message code mismatch: have %v, want %v", decodedMsg.Code, ibfttypes.MsgCommit)
		}
		var m *istanbul.Subject
		err = decodedMsg.Decode(&m)
		if err != nil {
			t.Errorf("error mismatch: have %v, want nil", err)
		}
		if !reflect.DeepEqual(m, expectedSubject) {
			t.Errorf("subject mismatch: have %v, want %v", m, expectedSubject)
		}
		if !r0.current.IsHashLocked() {
			t.Errorf("block should be locked")
		}
	}
}

// round is not checked for now
func TestVerifyPrepare(t *testing.T) {
	// for log purpose
	privateKey, _ := crypto.GenerateKey()
	peer := validator.New(getPublicKeyAddress(privateKey))
	valSet := validator.NewSet([]common.Address{peer.Address()}, istanbul.NewRoundRobinProposerPolicy())

	sys := NewTestSystemWithBackend(uint64(1), uint64(0))

	testCases := []struct {
		expected error

		prepare    *istanbul.Subject
		roundState *roundState
	}{
		{
			// normal case
			expected: nil,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
		{
			// old ibfttypes.Message
			expected: istanbulcommon.ErrInconsistentSubject,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// different digest
			expected: istanbulcommon.ErrInconsistentSubject,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				Digest: common.StringToHash("1234567890"),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// malicious package(lack of sequence)
			expected: istanbulcommon.ErrInconsistentSubject,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: nil},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(1)},
				valSet,
			),
		},
		{
			// wrong PREPARE ibfttypes.Message with same sequence but different round
			expected: istanbulcommon.ErrInconsistentSubject,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(1), Sequence: big.NewInt(0)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
		{
			// wrong PREPARE ibfttypes.Message with same round but different sequence
			expected: istanbulcommon.ErrInconsistentSubject,
			prepare: &istanbul.Subject{
				View:   &istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(1)},
				Digest: newTestProposal().Hash(),
			},
			roundState: newTestRoundState(
				&istanbul.View{Round: big.NewInt(0), Sequence: big.NewInt(0)},
				valSet,
			),
		},
	}
	for i, test := range testCases {
		c := sys.backends[0].engine
		c.current = test.roundState

		if err := c.verifyPrepare(test.prepare, peer); err != nil {
			if err != test.expected {
				t.Errorf("result %d: error mismatch: have %v, want %v", i, err, test.expected)
			}
		}
	}
}
