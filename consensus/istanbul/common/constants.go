package istanbulcommon

import (
	"math/big"

	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/core/types"
)

var (
	DefaultDifficulty = big.NewInt(1)
	NilUncleHash      = types.CalcUncleHash(nil) // Always Keccak256(RLP([])) as uncles are meaningless outside of PoW.
	EmptyBlockNonce   = types.BlockNonce{}
	NonceAuthVote     = hexutil.MustDecode("0xffffffffffffffff") // Magic nonce number to vote on adding a new validator
	NonceDropVote     = hexutil.MustDecode("0x0000000000000000") // Magic nonce number to vote on removing a validator.
)
