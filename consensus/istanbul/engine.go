package istanbul

import (
	"math/big"
	"time"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus"
	"github.com/ethereum/go-ethereum/core/state"
	"github.com/ethereum/go-ethereum/core/types"
)

type Engine interface {
	Address() common.Address
	Author(header *types.Header) (common.Address, error)
	Validators(header *types.Header) ([]common.Address, error)
	Signers(header *types.Header) ([]common.Address, error)
	CommitHeader(header *types.Header, seals [][]byte, round *big.Int) error
	VerifyBlockProposal(chain consensus.ChainHeaderReader, block *types.Block, validators ValidatorSet) (time.Duration, error)
	VerifyHeader(chain consensus.ChainHeaderReader, header *types.Header, parents []*types.Header, validators ValidatorSet) error
	VerifyUncles(chain consensus.ChainReader, block *types.Block) error
	VerifySeal(chain consensus.ChainHeaderReader, header *types.Header, validators ValidatorSet) error
	Prepare(chain consensus.ChainHeaderReader, header *types.Header, validators ValidatorSet) error
	Finalize(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header)
	FinalizeAndAssemble(chain consensus.ChainHeaderReader, header *types.Header, state *state.StateDB, txs []*types.Transaction, uncles []*types.Header, receipts []*types.Receipt) (*types.Block, error)
	Seal(chain consensus.ChainHeaderReader, block *types.Block, validators ValidatorSet) (*types.Block, error)
	SealHash(header *types.Header) common.Hash
	CalcDifficulty(chain consensus.ChainHeaderReader, time uint64, parent *types.Header) *big.Int
	WriteVote(header *types.Header, candidate common.Address, authorize bool) error
	ReadVote(header *types.Header) (candidate common.Address, authorize bool, err error)
}
