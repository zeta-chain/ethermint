package types

import (
	"math/big"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/params"
)

// MakeSigner returns a Signer based on the given chain config and block number.
// We use this instead of ethtypes.MakeSigner because cosmos always uses blockNumber for hard forks
func MakeSigner(config *params.ChainConfig, blockNumber *big.Int) ethtypes.Signer {
	var signer ethtypes.Signer
	switch {
	case config.IsLondon(blockNumber):
		signer = ethtypes.NewLondonSigner(config.ChainID)
	case config.IsBerlin(blockNumber):
		signer = ethtypes.NewEIP2930Signer(config.ChainID)
	case config.IsEIP155(blockNumber):
		signer = ethtypes.NewEIP155Signer(config.ChainID)
	case config.IsHomestead(blockNumber):
		signer = ethtypes.HomesteadSigner{}
	default:
		signer = ethtypes.FrontierSigner{}
	}
	return signer
}
