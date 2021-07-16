package qbfttypes

import (
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
)

// QBFT message codes
const (
	PreprepareCode  = 0x12
	PrepareCode     = 0x13
	CommitCode      = 0x14
	RoundChangeCode = 0x15
)

// A set containing the messages codes for all QBFT messages.
func MessageCodes() map[uint64]struct{} {
	return map[uint64]struct{}{
		PreprepareCode:  {},
		PrepareCode:     {},
		CommitCode:      {},
		RoundChangeCode: {},
	}
}

// Common interface for all QBFT messages
type QBFTMessage interface {
	Code() uint64
	View() istanbul.View
	Source() common.Address
	SetSource(address common.Address)
	EncodePayloadForSigning() ([]byte, error)
	Signature() []byte
	SetSignature(signature []byte)
}
