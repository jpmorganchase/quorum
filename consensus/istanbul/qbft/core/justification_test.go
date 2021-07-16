package core

import (
	"fmt"
	"math/big"
	"math/rand"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	qbfttypes "github.com/ethereum/go-ethereum/consensus/istanbul/qbft/types"
	"github.com/ethereum/go-ethereum/consensus/istanbul/validator"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/crypto"
)

// Tests combinations of justifications that evaluate to true.
func TestJustifyTrue(t *testing.T) {
	for quorumSize := 3; quorumSize <= 10; quorumSize++ {
		// All ROUND-CHANGE messages have pr/pb nil
		testParameterizedCase(t, quorumSize, quorumSize, 0, 0, 0, 0, 0, true)

		// Some ROUND-CHANGE message has pr/pb not nil
		for equal := 1; equal <= quorumSize; equal++ {
			for less := 0; less <= quorumSize-equal; less++ {
				nil := quorumSize - equal - less
				testParameterizedCase(t, quorumSize, nil, equal, less, 0, quorumSize, 0, true)
			}
		}
	}
}

// Tests combinations of justifications that evaluate to false.
func TestJustifyFalse(t *testing.T) {
	for quorumSize := 3; quorumSize <= 10; quorumSize++ {
		// Total ROUND-CHANGE messages less than quorumSize
		// all have pr/pb nil
		for totalRoundChange := 0; totalRoundChange < quorumSize; totalRoundChange++ {
			testParameterizedCase(t, quorumSize, totalRoundChange, 0, 0, 0, 0, 0, false)
		}
		// some has pr/pb not nil
		for totalRoundChange := 0; totalRoundChange < quorumSize; totalRoundChange++ {
			for equal := 1; equal <= totalRoundChange; equal++ {
				for less := 0; less <= totalRoundChange-equal; less++ {
					nil := totalRoundChange - equal - less
					testParameterizedCase(t, quorumSize, nil, equal, less, 0, quorumSize, 0, false)
				}
			}
		}

		// Total ROUND-CHANGE messages equal to quorumSize
		for equal := 1; equal <= quorumSize; equal++ {
			for less := 0; less <= quorumSize-equal; less++ {
				nil := quorumSize - equal - less

				// Total PREPARE messages less than quorumSize
				for total := 0; total < quorumSize; total++ {
					testParameterizedCase(t, quorumSize, nil, equal, less, 0, total, quorumSize-total, false)
				}

				// Total PREPARE messages equal to quorumSize and some PREPARE message has round different than others
				for different := 1; different <= quorumSize; different++ {
					testParameterizedCase(t, quorumSize, nil, equal, less, 0, quorumSize-different, different, false)
				}
			}
		}
	}
}

func testParameterizedCase(
	t *testing.T,
	quorumSize int,
	rcForNil int,
	rcEqualToTargetRound int,
	rcLowerThanTargetRound int,
	rcHigherThanTargetRound int,
	preparesForTargetRound int,
	preparesNotForTargetRound int,
	messageJustified bool) {

	pp := istanbul.NewRoundRobinProposerPolicy()
	pp.Use(istanbul.ValidatorSortByByte())
	validatorSet := validator.NewSet(generateValidators(quorumSize), pp)
	block := makeBlock(1)
	var round int64 = 10
	var targetPreparedRound int64 = 5

	rng := rand.New(rand.NewSource(time.Now().UTC().UnixNano()))

	if rcForNil+rcEqualToTargetRound+rcLowerThanTargetRound+rcHigherThanTargetRound > quorumSize {
		t.Errorf("rcForNil (%v) + rcEqualToTargetRound (%v) + rcLowerThanTargetRound (%v) + rcHigherThanTargetRound (%v) > quorumSize (%v)",
			rcForNil, rcEqualToTargetRound, rcLowerThanTargetRound, rcHigherThanTargetRound, quorumSize)
	}

	if preparesForTargetRound+preparesNotForTargetRound > quorumSize {
		t.Errorf("preparesForTargetRound (%v) + preparesNotForTargetRound (%v) > quorumSize (%v)", preparesForTargetRound, preparesNotForTargetRound, quorumSize)
	}

	// ROUND-CHANGE messages
	roundChangeMessages := make([]*qbfttypes.SignedRoundChangePayload, 0)
	for index, validator := range validatorSet.List() {
		var m *qbfttypes.SignedRoundChangePayload
		if index < rcForNil {
			m = createRoundChangeMessage(validator.Address(), round, 0, nil)
		} else if index >= rcForNil && index < rcForNil+rcEqualToTargetRound {
			m = createRoundChangeMessage(validator.Address(), round, targetPreparedRound, block)
		} else if index >= rcForNil+rcEqualToTargetRound && index < rcForNil+rcEqualToTargetRound+rcLowerThanTargetRound {
			m = createRoundChangeMessage(validator.Address(), round, int64(rng.Intn(int(targetPreparedRound)-1)+1), block)
		} else if index >= rcForNil+rcEqualToTargetRound+rcLowerThanTargetRound && index < rcForNil+rcEqualToTargetRound+rcLowerThanTargetRound+rcHigherThanTargetRound {
			m = createRoundChangeMessage(validator.Address(), round, int64(rng.Intn(int(targetPreparedRound))+int(targetPreparedRound)+1), block)
		} else {
			break
		}
		roundChangeMessages = append(roundChangeMessages, m)
	}

	// PREPARE messages
	prepareMessages := make([]*qbfttypes.Prepare, 0)
	for index, validator := range validatorSet.List() {
		var m *qbfttypes.Prepare
		if index < preparesForTargetRound {
			m = createPrepareMessage(validator.Address(), targetPreparedRound, block)
		} else if index >= preparesForTargetRound && index < preparesForTargetRound+preparesNotForTargetRound {
			notTargetPreparedRound := targetPreparedRound
			for notTargetPreparedRound == targetPreparedRound {
				notTargetPreparedRound = rng.Int63()
			}
			m = createPrepareMessage(validator.Address(), notTargetPreparedRound, block)
		} else {
			break
		}
		prepareMessages = append(prepareMessages, m)
	}

	for _, m := range roundChangeMessages {
		fmt.Printf("RC %v\n", m)
	}
	for _, m := range prepareMessages {
		fmt.Printf("PR %v\n", m)
	}
	fmt.Println("roundChangeMessages", roundChangeMessages, len(roundChangeMessages))
	if err := isJustified(block, roundChangeMessages, prepareMessages, quorumSize); err == nil && !messageJustified {
		t.Errorf("quorumSize = %v, rcForNil = %v, rcEqualToTargetRound = %v, rcLowerThanTargetRound = %v, rcHigherThanTargetRound = %v, preparesForTargetRound = %v, preparesNotForTargetRound = %v (Expected: %v, Actual: %v)",
			quorumSize, rcForNil, rcEqualToTargetRound, rcLowerThanTargetRound, rcHigherThanTargetRound, preparesForTargetRound, preparesNotForTargetRound, err == nil, !messageJustified)

	}
}

func createRoundChangeMessage(from common.Address, round int64, preparedRound int64, preparedBlock istanbul.Proposal) *qbfttypes.SignedRoundChangePayload {
	m := qbfttypes.NewRoundChange(big.NewInt(1), big.NewInt(1), big.NewInt(preparedRound), preparedBlock)
	m.SetSource(from)
	return &m.SignedRoundChangePayload
}

func createPrepareMessage(from common.Address, round int64, preparedBlock istanbul.Proposal) *qbfttypes.Prepare {
	return qbfttypes.NewPrepareWithSigAndSource(big.NewInt(1), big.NewInt(round), preparedBlock.Hash(), nil, from)
}

func generateValidators(n int) []common.Address {
	vals := make([]common.Address, 0)
	for i := 0; i < n; i++ {
		privateKey, _ := crypto.GenerateKey()
		vals = append(vals, crypto.PubkeyToAddress(privateKey.PublicKey))
	}
	return vals
}

func makeBlock(number int64) *types.Block {
	header := &types.Header{
		Difficulty: big.NewInt(0),
		Number:     big.NewInt(number),
		GasLimit:   0,
		GasUsed:    0,
		Time:       0,
	}
	block := &types.Block{}
	return block.WithSeal(header)
}
