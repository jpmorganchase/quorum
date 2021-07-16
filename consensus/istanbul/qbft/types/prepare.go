package qbfttypes

import (
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/rlp"
)

// A QBFT PREPARE message.
type Prepare struct {
	CommonPayload
	Digest common.Hash
}

func NewPrepare(sequence *big.Int, round *big.Int, digest common.Hash) *Prepare {
	return &Prepare{
		CommonPayload: CommonPayload{
			code:     PrepareCode,
			Sequence: sequence,
			Round:    round,
		},
		Digest: digest,
	}
}

func NewPrepareWithSigAndSource(sequence *big.Int, round *big.Int, digest common.Hash, signature []byte, source common.Address) *Prepare {
	prepare := NewPrepare(sequence, round, digest)
	prepare.signature = signature
	prepare.source = source
	return prepare
}

func (p *Prepare) String() string {
	return fmt.Sprintf("Prepare {seq=%v, round=%v, digest=%v}", p.Sequence, p.Round, p.Digest.Hex())
}

func (p *Prepare) EncodePayloadForSigning() ([]byte, error) {
	return rlp.EncodeToBytes(
		[]interface{}{
			p.Code(),
			[]interface{}{p.Sequence, p.Round, p.Digest},
		})
}

func (p *Prepare) EncodeRLP(w io.Writer) error {
	return rlp.Encode(
		w,
		[]interface{}{
			[]interface{}{
				p.Sequence,
				p.Round,
				p.Digest},
			p.signature,
		})
}

func (p *Prepare) DecodeRLP(stream *rlp.Stream) error {
	var message struct {
		Payload struct {
			Sequence *big.Int
			Round    *big.Int
			Digest   common.Hash
		}
		Signature []byte
	}
	if err := stream.Decode(&message); err != nil {
		return err
	}
	p.code = PrepareCode
	p.Sequence = message.Payload.Sequence
	p.Round = message.Payload.Round
	p.Digest = message.Payload.Digest
	p.signature = message.Signature
	return nil
}
