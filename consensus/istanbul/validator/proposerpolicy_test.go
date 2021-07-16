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

package validator

import (
	"testing"

	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/consensus/istanbul"
	"github.com/stretchr/testify/assert"
)

func TestProposerPolicy(t *testing.T) {
	addr1 := common.HexToAddress("0xc53f2189bf6d7bf56722731787127f90d319e112")
	addr2 := common.HexToAddress("0xed2d479591fe2c5626ce09bca4ed2a62e00e5bc2")
	addr3 := common.HexToAddress("0xc8417f834995aaeb35f342a67a4961e19cd4735c")
	addr4 := common.HexToAddress("0x784ae51f5013b51c8360afdf91c6bc5a16f586ea")
	addr5 := common.HexToAddress("0xecf0974e6f0630fd91ea4da8399cdb3f59e5220f")
	addr6 := common.HexToAddress("0x411c4d11acd714b82a5242667e36de14b9e1d10b")

	addrSet := []common.Address{addr1, addr2, addr3, addr4, addr5, addr6}
	addressSortedByByte := []common.Address{addr6, addr4, addr1, addr3, addr5, addr2}
	addressSortedByString := []common.Address{addr6, addr4, addr1, addr2, addr5, addr3}

	pp := istanbul.NewRoundRobinProposerPolicy()
	pp.Use(istanbul.ValidatorSortByByte())

	valSet := NewSet(addrSet, pp)
	valList := valSet.List()

	for i := 0; i < 6; i++ {
		assert.Equal(t, addressSortedByByte[i].Hex(), valList[i].String(), "validatorSet not byte sorted")
	}

	pp.Use(istanbul.ValidatorSortByString())
	for i := 0; i < 6; i++ {
		assert.Equal(t, addressSortedByString[i].Hex(), valList[i].String(), "validatorSet not string sorted")
	}

}
