package qbfttypes

import (
	"bytes"
	"fmt"
	"io"
	"math/big"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	istanbulcommon "github.com/ethereum/go-ethereum/consensus/istanbul/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/log"
	"github.com/ethereum/go-ethereum/rlp"
)

// ROUND-CHANGE
type RoundChange struct {
	SignedRoundChangePayload
	PreparedBlock *types.Block
	Justification []*Prepare
}

func NewRoundChange(sequence *big.Int, round *big.Int, preparedRound *big.Int, preparedBlock istanbul.Proposal) *RoundChange {
	roundChange := &RoundChange{
		SignedRoundChangePayload: SignedRoundChangePayload{
			CommonPayload: CommonPayload{
				code:     RoundChangeCode,
				Sequence: sequence,
				Round:    round,
			},
			PreparedRound:  preparedRound,
			PreparedDigest: common.Hash{},
		},
	}

	if preparedBlock != nil {
		roundChange.PreparedBlock = preparedBlock.(*types.Block)
		roundChange.PreparedDigest = preparedBlock.Hash()
	}

	return roundChange
}

type SignedRoundChangePayload struct {
	CommonPayload
	PreparedRound  *big.Int
	PreparedDigest common.Hash
}

func (p *SignedRoundChangePayload) String() string {
	return fmt.Sprintf("RoundChange {seq=%v, round=%v, pr=%v, pv=%v}",
		p.Sequence, p.Round, p.PreparedRound, p.PreparedDigest.Hex())
}

func (p *SignedRoundChangePayload) EncodeRLP(w io.Writer) error {
	var encodedPayload rlp.RawValue
	encodedPayload, err := p.encodePayloadInternal()
	if err != nil {
		return err
	}

	return rlp.Encode(
		w,
		[]interface{}{encodedPayload, p.signature})

}

func (p *SignedRoundChangePayload) DecodeRLP(stream *rlp.Stream) error {
	// Signed Payload
	if _, err := stream.List(); err != nil {
		log.Error("QBFT: Error List() Signed Payload", "err", err)
		return err
	}

	// Payload
	encodedPayload, err := stream.Raw()
	if err != nil {
		log.Error("QBFT: Error Raw()", "err", err)
		return err
	}

	payloadStream := rlp.NewStream(bytes.NewReader(encodedPayload), 0)

	if _, err = payloadStream.List(); err != nil {
		log.Error("QBFT: Error List() Payload", "err", err)
		return err
	}

	if err = payloadStream.Decode(&p.Sequence); err != nil {
		log.Error("QBFT: Error Decode(&m.Sequence)", "err", err)
		return err
	}
	if err = payloadStream.Decode(&p.Round); err != nil {
		log.Error("QBFT: Error Decode(&m.Round)", "err", err)
		return err
	}

	// Prepared
	var size uint64
	if size, err = payloadStream.List(); err != nil {
		log.Error("QBFT: Error List() Prepared", "err", err)
		return err
	}
	if size > 0 {
		if err = payloadStream.Decode(&p.PreparedRound); err != nil {
			log.Error("QBFT: Error Decode(&m.PreparedRound)", "err", err)
			return err
		}
		if err = payloadStream.Decode(&p.PreparedDigest); err != nil {
			log.Error("QBFT: Error Decode(&p.PreparedDigest)", "err", err)
			return err
		}
	}
	// End Prepared
	if err = payloadStream.ListEnd(); err != nil {
		return err
	}

	// End Payload
	if err = payloadStream.ListEnd(); err != nil {
		return err
	}

	if err = stream.Decode(&p.signature); err != nil {
		return err
	}
	// End SignedPayload
	if err = stream.ListEnd(); err != nil {
		return err
	}

	p.code = RoundChangeCode

	log.Info("QBFT: Correctly decoded SignedRoundChangePayload", "p", p)

	return nil
}

func (p *SignedRoundChangePayload) encodePayloadInternal() ([]byte, error) {
	var prepared = []interface{}{}
	if p.PreparedRound != nil && !common.EmptyHash(p.PreparedDigest) {
		prepared = []interface{}{p.PreparedRound, p.PreparedDigest}
	}
	return rlp.EncodeToBytes(
		[]interface{}{
			p.Sequence,
			p.Round,
			prepared})
}

func (p *SignedRoundChangePayload) EncodePayloadForSigning() ([]byte, error) {
	var encodedPayload rlp.RawValue
	encodedPayload, err := p.encodePayloadInternal()
	if err != nil {
		return nil, err
	}

	return rlp.EncodeToBytes(
		[]interface{}{
			p.Code(),
			encodedPayload,
		})
}

func (m *RoundChange) EncodeRLP(w io.Writer) error {
	var encodedPayload rlp.RawValue
	encodedPayload, err := m.encodePayloadInternal()
	if err != nil {
		return err
	}

	return rlp.Encode(
		w,
		[]interface{}{
			[]interface{}{
				encodedPayload,
				m.signature,
			},
			m.PreparedBlock, m.Justification,
		})
}

func (m *RoundChange) DecodeRLP(stream *rlp.Stream) error {
	var err error

	// RoundChange Message
	if _, err = stream.List(); err != nil {
		return err
	}

	// Signed Payload
	if _, err = stream.List(); err != nil {
		log.Error("QBFT: Error List() Signed Payload", "err", err)
		return err
	}

	// Payload
	encodedPayload, err := stream.Raw()
	if err != nil {
		log.Error("QBFT: Error Raw()", "err", err)
		return err
	}

	payloadStream := rlp.NewStream(bytes.NewReader(encodedPayload), 0)

	if _, err = payloadStream.List(); err != nil {
		log.Error("QBFT: Error List() Payload", "err", err)
		return err
	}

	if err = payloadStream.Decode(&m.Sequence); err != nil {
		log.Error("QBFT: Error Decode(&m.Sequence)", "err", err)
		return err
	}
	if err = payloadStream.Decode(&m.Round); err != nil {
		log.Error("QBFT: Error Decode(&m.Round)", "err", err)
		return err
	}

	// Prepared
	var size uint64
	if size, err = payloadStream.List(); err != nil {
		log.Error("QBFT: Error List() Prepared", "err", err)
		return err
	}
	if size > 0 {
		if err = payloadStream.Decode(&m.PreparedRound); err != nil {
			log.Error("QBFT: Error Decode(&m.PreparedRound)", "err", err)
			return err
		}
		if err = payloadStream.Decode(&m.PreparedDigest); err != nil {
			log.Error("QBFT: Error Decode(&m.PreparedDigest)", "err", err)
			return err
		}
	}
	// End Prepared
	if err = payloadStream.ListEnd(); err != nil {
		return err
	}

	// End Payload
	if err = payloadStream.ListEnd(); err != nil {
		return err
	}

	if err = stream.Decode(&m.signature); err != nil {
		return err
	}
	// End SignedPayload
	if err = stream.ListEnd(); err != nil {
		return err
	}

	if _, size, err = stream.Kind(); err != nil {
		log.Error("QBFT: Error Kind()", "err", err)
		return err
	}
	if size == 0 {
		if _, err = stream.Raw(); err != nil {
			log.Error("QBFT: Error Raw()", "err", err)
			return err
		}
	} else {
		if err = stream.Decode(&m.PreparedBlock); err != nil {
			log.Error("QBFT: Error Decode(&m.PreparedDigest)", "err", err)
			return err
		}
		if m.PreparedBlock.Hash() != m.PreparedDigest {
			log.Error("QBFT: Error m.PreparedDigest.Hash() != digest")
			return istanbulcommon.ErrFailedDecodePreprepare
		}
	}

	if _, size, err = stream.Kind(); err != nil {
		log.Error("QBFT: Error Kind()", "err", err)
		return err
	}
	if size == 0 {
		if _, err = stream.Raw(); err != nil {
			log.Error("QBFT: Error Raw()", "err", err)
			return err
		}
	} else {
		if err = stream.Decode(&m.Justification); err != nil {
			log.Error("QBFT: Error Decode(&m.Justification)", "err", err)
			return err
		}
	}

	// End RoundChange Message
	if err = stream.ListEnd(); err != nil {
		return err
	}

	m.code = RoundChangeCode

	return nil
}
