package app

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	mempool "github.com/cosmos/cosmos-sdk/types/mempool"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	evmtypes "github.com/zeta-chain/ethermint/x/evm/types"
)

var _ mempool.SignerExtractionAdapter = EthSignerExtractionAdapter{}

// EthSignerExtractionAdapter is the default implementation of SignerExtractionAdapter. It extracts the signers
// from a cosmos-sdk tx via GetSignaturesV2.
type EthSignerExtractionAdapter struct {
	fallback mempool.SignerExtractionAdapter
}

// NewEthSignerExtractionAdapter constructs a new EthSignerExtractionAdapter instance
func NewEthSignerExtractionAdapter(fallback mempool.SignerExtractionAdapter) EthSignerExtractionAdapter {
	return EthSignerExtractionAdapter{fallback}
}

// GetSigners implements the Adapter interface
// NOTE: only the first item is used by the mempool
func (s EthSignerExtractionAdapter) GetSigners(tx sdk.Tx) ([]mempool.SignerData, error) {
	if txWithExtensions, ok := tx.(authante.HasExtensionOptionsTx); ok {
		opts := txWithExtensions.GetExtensionOptions()
		if len(opts) > 0 && opts[0].GetTypeUrl() == "/ethermint.evm.v1.ExtensionOptionsEthereumTx" {
			for _, msg := range tx.GetMsgs() {
				if ethMsg, ok := msg.(*evmtypes.MsgEthereumTx); ok {
					txData, err := evmtypes.UnpackTxData(ethMsg.Data)
					if err != nil {
						return nil, err
					}
					return []mempool.SignerData{
						mempool.NewSignerData(
							sdk.AccAddress(ethMsg.From),
							txData.GetNonce(),
						),
					}, nil
				}
			}
		}
	}

	return s.fallback.GetSigners(tx)
}
