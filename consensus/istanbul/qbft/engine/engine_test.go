package qbftengine

import (
	"bytes"
	"math/big"
	"reflect"
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/common/hexutil"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	"github.com/ethereum/go-ethereum/core/types"
)

func TestPrepareExtra(t *testing.T) {
	validators := make([]common.Address, 4)
	validators[0] = common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f067778eaf8a"))
	validators[1] = common.BytesToAddress(hexutil.MustDecode("0x294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212"))
	validators[2] = common.BytesToAddress(hexutil.MustDecode("0x6beaaed781d2d2ab6350f5c4566a2c6eaac407a6"))
	validators[3] = common.BytesToAddress(hexutil.MustDecode("0x8be76812f765c24641ec63dc2852b378aba2b440"))

	expectedResult := hexutil.MustDecode("0xf87aa00000000000000000000000000000000000000000000000000000000000000000f8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b440c080c0")

	h := &types.Header{}
	err := ApplyHeaderQBFTExtra(
		h,
		WriteValidators(validators),
	)
	if err != nil {
		t.Errorf("error mismatch: have %v, want: nil", err)
	}
	if !reflect.DeepEqual(h.Extra, expectedResult) {
		t.Errorf("payload mismatch: have %v, want %v", h.Extra, expectedResult)
	}
}

func TestWriteCommittedSeals(t *testing.T) {
	istRawData := hexutil.MustDecode("0xf85a80f8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b440c080c0")
	expectedCommittedSeal := append([]byte{1, 2, 3}, bytes.Repeat([]byte{0x00}, types.IstanbulExtraSeal-3)...)
	expectedIstExtra := &types.QBFTExtra{
		VanityData: []byte{},
		Validators: []common.Address{
			common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f067778eaf8a")),
			common.BytesToAddress(hexutil.MustDecode("0x294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212")),
			common.BytesToAddress(hexutil.MustDecode("0x6beaaed781d2d2ab6350f5c4566a2c6eaac407a6")),
			common.BytesToAddress(hexutil.MustDecode("0x8be76812f765c24641ec63dc2852b378aba2b440")),
		},
		CommittedSeal: [][]byte{expectedCommittedSeal},
		Round:         0,
		Vote:          nil,
	}

	h := &types.Header{
		Extra: istRawData,
	}

	// normal case
	err := ApplyHeaderQBFTExtra(
		h,
		writeCommittedSeals([][]byte{expectedCommittedSeal}),
	)
	if err != nil {
		t.Errorf("error mismatch: have %v, want: nil", err)
	}

	// verify istanbul extra-data
	istExtra, err := getExtra(h)
	if err != nil {
		t.Errorf("error mismatch: have %v, want nil", err)
	}
	if !reflect.DeepEqual(istExtra, expectedIstExtra) {
		t.Errorf("extra data mismatch: have %v, want %v", istExtra, expectedIstExtra)
	}

	// invalid seal
	unexpectedCommittedSeal := append(expectedCommittedSeal, make([]byte, 1)...)
	err = ApplyHeaderQBFTExtra(
		h,
		writeCommittedSeals([][]byte{unexpectedCommittedSeal}),
	)
	if err != istanbulcommon.ErrInvalidCommittedSeals {
		t.Errorf("error mismatch: have %v, want %v", err, istanbulcommon.ErrInvalidCommittedSeals)
	}
}

func TestWriteRoundNumber(t *testing.T) {
	istRawData := hexutil.MustDecode("0xf85a80f8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b440c005c0")
	expectedIstExtra := &types.QBFTExtra{
		VanityData: []byte{},
		Validators: []common.Address{
			common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f067778eaf8a")),
			common.BytesToAddress(hexutil.MustDecode("0x294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212")),
			common.BytesToAddress(hexutil.MustDecode("0x6beaaed781d2d2ab6350f5c4566a2c6eaac407a6")),
			common.BytesToAddress(hexutil.MustDecode("0x8be76812f765c24641ec63dc2852b378aba2b440")),
		},
		CommittedSeal: [][]byte{},
		Round:         5,
		Vote:          nil,
	}

	var expectedErr error

	h := &types.Header{
		Extra: istRawData,
	}

	// normal case
	err := ApplyHeaderQBFTExtra(
		h,
		writeRoundNumber(big.NewInt(5)),
	)
	if err != expectedErr {
		t.Errorf("error mismatch: have %v, want %v", err, expectedErr)
	}

	// verify istanbul extra-data
	istExtra, err := getExtra(h)
	if err != nil {
		t.Errorf("error mismatch: have %v, want nil", err)
	}
	if !reflect.DeepEqual(istExtra, expectedIstExtra) {
		t.Errorf("extra data mismatch: have %v, want %v", istExtra.VanityData, expectedIstExtra.VanityData)
	}
}

func TestWriteValidatorVote(t *testing.T) {
	vanity := bytes.Repeat([]byte{0x00}, types.IstanbulExtraVanity)
	istRawData := hexutil.MustDecode("0xf85a80f8549444add0ec310f115a0e603b2d7db9f067778eaf8a94294fc7e8f22b3bcdcf955dd7ff3ba2ed833f8212946beaaed781d2d2ab6350f5c4566a2c6eaac407a6948be76812f765c24641ec63dc2852b378aba2b440c005c0")
	vote := &types.ValidatorVote{RecipientAddress: common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f06777123456")), VoteType: types.QBFTAuthVote}
	expectedIstExtra := &types.QBFTExtra{
		VanityData:    vanity,
		Validators:    []common.Address{},
		CommittedSeal: [][]byte{},
		Round:         0,
		Vote:          vote,
	}

	var expectedErr error

	h := &types.Header{
		Extra: istRawData,
	}

	// normal case
	err := ApplyHeaderQBFTExtra(
		h,
		WriteVote(common.BytesToAddress(hexutil.MustDecode("0x44add0ec310f115a0e603b2d7db9f06777123456")), true),
	)
	if err != expectedErr {
		t.Errorf("error mismatch: have %v, want %v", err, expectedErr)
	}

	// verify istanbul extra-data
	istExtra, err := getExtra(h)
	if err != nil {
		t.Errorf("error mismatch: have %v, want nil", err)
	}
	if !reflect.DeepEqual(istExtra.Vote, expectedIstExtra.Vote) {
		t.Errorf("extra data mismatch: have %v, want %v", istExtra, expectedIstExtra)
	}
}
