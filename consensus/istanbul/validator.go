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

package istanbul

import (
	"bytes"
	"sort"
	"strings"

	"github.com/ethereum/go-ethereum/common"
)

type Validator interface {
	// Address returns address
	Address() common.Address

	// String representation of Validator
	String() string
}

// ----------------------------------------------------------------------------

type Validators []Validator

func (vs validatorSorter) Len() int {
	return len(vs.validators)
}

func (vs validatorSorter) Swap(i, j int) {
	vs.validators[i], vs.validators[j] = vs.validators[j], vs.validators[i]
}

func (vs validatorSorter) Less(i, j int) bool {
	return vs.by(vs.validators[i], vs.validators[j])
}

type validatorSorter struct {
	validators Validators
	by         ValidatorSortByFunc
}

type ValidatorSortByFunc func(v1 Validator, v2 Validator) bool

func ValidatorSortByString() ValidatorSortByFunc {
	return func(v1 Validator, v2 Validator) bool {
		return strings.Compare(v1.String(), v2.String()) < 0
	}
}

func ValidatorSortByByte() ValidatorSortByFunc {
	return func(v1 Validator, v2 Validator) bool {
		return bytes.Compare(v1.Address().Bytes(), v2.Address().Bytes()) < 0
	}
}

func (by ValidatorSortByFunc) Sort(validators []Validator) {
	v := &validatorSorter{
		validators: validators,
		by:         by,
	}
	sort.Sort(v)
}

// ----------------------------------------------------------------------------

type ValidatorSet interface {
	// Calculate the proposer
	CalcProposer(lastProposer common.Address, round uint64)
	// Return the validator size
	Size() int
	// Return the validator array
	List() []Validator
	// Get validator by index
	GetByIndex(i uint64) Validator
	// Get validator by given address
	GetByAddress(addr common.Address) (int, Validator)
	// Get current proposer
	GetProposer() Validator
	// Check whether the validator with given address is a proposer
	IsProposer(address common.Address) bool
	// Add validator
	AddValidator(address common.Address) bool
	// Remove validator
	RemoveValidator(address common.Address) bool
	// Copy validator set
	Copy() ValidatorSet
	// Get the maximum number of faulty nodes
	F() int
	// Get proposer policy
	Policy() ProposerPolicy

	// SortValidators sorts the validators based on the configured By function
	SortValidators()
}

// ----------------------------------------------------------------------------

type ProposalSelector func(ValidatorSet, common.Address, uint64) Validator
