package qbftengine

import "github.com/ethereum/go-ethereum/core/types"

type ApplyQBFTExtra func(*types.QBFTExtra) error

func Combine(applies ...ApplyQBFTExtra) ApplyQBFTExtra {
	return func(extra *types.QBFTExtra) error {
		for _, apply := range applies {
			err := apply(extra)
			if err != nil {
				return err
			}
		}
		return nil
	}
}

func ApplyHeaderQBFTExtra(header *types.Header, applies ...ApplyQBFTExtra) error {
	extra, err := getExtra(header)
	if err != nil {
		return err
	}

	err = Combine(applies...)(extra)
	if err != nil {
		return err
	}

	return setExtra(header, extra)
}
