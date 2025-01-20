package keeper_test

import (
	"context"
	"encoding/json"
	"math/big"

	sdkmath "cosmossdk.io/math"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/zeta-chain/ethermint/app"
	"github.com/zeta-chain/ethermint/crypto/ethsecp256k1"
	"github.com/zeta-chain/ethermint/encoding"
	"github.com/zeta-chain/ethermint/tests"
	"github.com/zeta-chain/ethermint/testutil"
	"github.com/zeta-chain/ethermint/x/feemarket/types"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	dbm "github.com/cosmos/cosmos-db"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	evmtypes "github.com/zeta-chain/ethermint/x/evm/types"
)

var _ = Describe("Feemarket", func() {
	var privKey *ethsecp256k1.PrivKey

	Describe("Performing EVM transactions", func() {
		type txParams struct {
			gasLimit  uint64
			gasPrice  *big.Int
			gasFeeCap *big.Int
			gasTipCap *big.Int
			accesses  *ethtypes.AccessList
		}
		type getprices func() txParams

		Context("with MinGasPrices (feemarket param) < BaseFee (feemarket)", func() {
			var (
				baseFee      int64
				minGasPrices int64
			)

			BeforeEach(func() {
				baseFee = 10_000_000_000
				minGasPrices = baseFee - 5_000_000_000

				// Note that the tests run the same transactions with `gasLimit =
				// 100_000`. With the fee calculation `Fee = (baseFee + tip) * gasLimit`,
				// a `minGasPrices = 5_000_000_000` results in `minGlobalFee =
				// 500_000_000_000_000`
				privKey, _ = setupTestWithContext("1", sdkmath.LegacyNewDec(minGasPrices), sdkmath.NewInt(baseFee))
			})

			Context("during CheckTx", func() {
				DescribeTable("should accept transactions with gas Limit > 0",
					func(malleate getprices) {
						p := malleate()
						to := tests.GenerateAddress()
						msgEthereumTx := buildEthTx(privKey, &to, p.gasLimit, p.gasPrice, p.gasFeeCap, p.gasTipCap, p.accesses)
						res := checkEthTx(privKey, msgEthereumTx)
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{100000, big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{100000, nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
				DescribeTable("should not accept transactions with gas Limit > 0",
					func(malleate getprices) {
						p := malleate()
						to := tests.GenerateAddress()
						msgEthereumTx := buildEthTx(privKey, &to, p.gasLimit, p.gasPrice, p.gasFeeCap, p.gasTipCap, p.accesses)
						res := checkEthTx(privKey, msgEthereumTx)
						Expect(res.IsOK()).To(Equal(false), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{0, big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{0, nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
			})

			Context("during DeliverTx", func() {
				DescribeTable("should accept transactions with gas Limit > 0",
					func(malleate getprices) {
						p := malleate()
						to := tests.GenerateAddress()
						msgEthereumTx := buildEthTx(privKey, &to, p.gasLimit, p.gasPrice, p.gasFeeCap, p.gasTipCap, p.accesses)
						res := deliverTx(prepareEthTx(privKey, msgEthereumTx))
						Expect(res.IsOK()).To(Equal(true), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{100000, big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{100000, nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
				DescribeTable("should not accept transactions with gas Limit > 0",
					func(malleate getprices) {
						p := malleate()
						to := tests.GenerateAddress()
						msgEthereumTx := buildEthTx(privKey, &to, p.gasLimit, p.gasPrice, p.gasFeeCap, p.gasTipCap, p.accesses)
						res := checkEthTx(privKey, msgEthereumTx)
						Expect(res.IsOK()).To(Equal(false), "transaction should have succeeded", res.GetLog())
					},
					Entry("legacy tx", func() txParams {
						return txParams{0, big.NewInt(baseFee), nil, nil, nil}
					}),
					Entry("dynamic tx", func() txParams {
						return txParams{0, nil, big.NewInt(baseFee), big.NewInt(0), &ethtypes.AccessList{}}
					}),
				)
			})
		})
	})
})

// setupTestWithContext sets up a test chain with an example Cosmos send msg,
// given a local (validator config) and a gloabl (feemarket param) minGasPrice
func setupTestWithContext(valMinGasPrice string, minGasPrice sdkmath.LegacyDec, baseFee sdkmath.Int) (*ethsecp256k1.PrivKey, banktypes.MsgSend) {
	privKey, msg := setupTest(valMinGasPrice + s.denom)
	params := types.DefaultParams()
	params.MinGasPrice = minGasPrice
	s.app.FeeMarketKeeper.SetParams(s.ctx, params)
	s.app.FeeMarketKeeper.SetBaseFee(s.ctx, baseFee.BigInt())
	s.Commit()

	return privKey, msg
}

func setupTest(localMinGasPrices string) (*ethsecp256k1.PrivKey, banktypes.MsgSend) {
	setupChain(localMinGasPrices)

	privKey, address := generateKey()
	amount, ok := sdkmath.NewIntFromString("10000000000000000000")
	s.Require().True(ok)
	initBalance := sdk.Coins{sdk.Coin{
		Denom:  s.denom,
		Amount: amount,
	}}
	testutil.FundAccount(s.app.BankKeeper, s.ctx, address, initBalance)

	msg := banktypes.MsgSend{
		FromAddress: address.String(),
		ToAddress:   address.String(),
		Amount: sdk.Coins{sdk.Coin{
			Denom:  s.denom,
			Amount: sdkmath.NewInt(10000),
		}},
	}
	s.Commit()
	return privKey, msg
}

func setupChain(localMinGasPricesStr string) {
	// Initialize the app, so we can use SetMinGasPrices to set the
	// validator-specific min-gas-prices setting
	db := dbm.NewMemDB()
	newapp := app.NewEthermintApp(
		log.NewNopLogger(),
		db,
		nil,
		true,
		simtestutil.NewAppOptionsWithFlagHome(app.DefaultNodeHome),
		baseapp.SetMinGasPrices(localMinGasPricesStr),
		baseapp.SetChainID(app.ChainID),
	)

	genesisState := app.NewTestGenesisState(newapp.AppCodec())
	genesisState[types.ModuleName] = newapp.AppCodec().MustMarshalJSON(types.DefaultGenesisState())

	stateBytes, err := json.MarshalIndent(genesisState, "", "  ")
	s.Require().NoError(err)

	// Initialize the chain
	newapp.InitChain(
		&abci.RequestInitChain{
			ChainId:         app.ChainID,
			Validators:      []abci.ValidatorUpdate{},
			AppStateBytes:   stateBytes,
			ConsensusParams: app.DefaultConsensusParams,
		},
	)

	s.app = newapp
	s.SetupApp(false)
}

func generateKey() (*ethsecp256k1.PrivKey, sdk.AccAddress) {
	address, priv := tests.NewAddrKey()
	return priv.(*ethsecp256k1.PrivKey), sdk.AccAddress(address.Bytes())
}

func getNonce(addressBytes []byte) uint64 {
	return s.app.EvmKeeper.GetNonce(
		s.ctx,
		common.BytesToAddress(addressBytes),
	)
}

func buildEthTx(
	priv *ethsecp256k1.PrivKey,
	to *common.Address,
	gasLimit uint64,
	gasPrice *big.Int,
	gasFeeCap *big.Int,
	gasTipCap *big.Int,
	accesses *ethtypes.AccessList,
) *evmtypes.MsgEthereumTx {
	chainID := s.app.EvmKeeper.ChainID()
	from := common.BytesToAddress(priv.PubKey().Address().Bytes())
	nonce := getNonce(from.Bytes())
	data := make([]byte, 0)
	msgEthereumTx := evmtypes.NewTx(
		chainID,
		nonce,
		to,
		nil,
		gasLimit,
		gasPrice,
		gasFeeCap,
		gasTipCap,
		data,
		accesses,
	)
	msgEthereumTx.From = from.String()
	return msgEthereumTx
}

func prepareEthTx(priv *ethsecp256k1.PrivKey, msgEthereumTx *evmtypes.MsgEthereumTx) []byte {
	encodingConfig := encoding.MakeConfig()
	option, err := codectypes.NewAnyWithValue(&evmtypes.ExtensionOptionsEthereumTx{})
	s.Require().NoError(err)

	txBuilder := encodingConfig.TxConfig.NewTxBuilder()
	builder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
	s.Require().True(ok)
	builder.SetExtensionOptions(option)

	err = msgEthereumTx.Sign(s.ethSigner, tests.NewSigner(priv))
	s.Require().NoError(err)

	// A valid msg should have empty `From`
	msgEthereumTx.From = ""
	err = txBuilder.SetMsgs(msgEthereumTx)
	s.Require().NoError(err)

	txData, err := evmtypes.UnpackTxData(msgEthereumTx.Data)
	s.Require().NoError(err)

	evmDenom := s.app.EvmKeeper.GetParams(s.ctx).EvmDenom
	fees := sdk.Coins{{Denom: evmDenom, Amount: sdkmath.NewIntFromBigInt(txData.Fee())}}
	builder.SetFeeAmount(fees)
	builder.SetGasLimit(msgEthereumTx.GetGas())

	// bz are bytes to be broadcasted over the network
	bz, err := encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	s.Require().NoError(err)

	return bz
}

func checkEthTx(priv *ethsecp256k1.PrivKey, msgEthereumTx *evmtypes.MsgEthereumTx) abci.ResponseCheckTx {
	bz := prepareEthTx(priv, msgEthereumTx)
	req := abci.RequestCheckTx{Tx: bz}
	res, _ := s.app.BaseApp.CheckTx(&req)
	return *res
}

func prepareCosmosTx(priv *ethsecp256k1.PrivKey, gasPrice *sdkmath.Int, msgs ...sdk.Msg) []byte {
	encodingConfig := encoding.MakeConfig()
	accountAddress := sdk.AccAddress(priv.PubKey().Address().Bytes())

	txBuilder := encodingConfig.TxConfig.NewTxBuilder()

	txBuilder.SetGasLimit(1000000)
	if gasPrice == nil {
		_gasPrice := sdkmath.NewInt(1)
		gasPrice = &_gasPrice
	}
	fees := &sdk.Coins{{Denom: s.denom, Amount: gasPrice.MulRaw(1000000)}}
	txBuilder.SetFeeAmount(*fees)
	err := txBuilder.SetMsgs(msgs...)
	s.Require().NoError(err)

	seq, err := s.app.AccountKeeper.GetSequence(s.ctx, accountAddress)
	s.Require().NoError(err)

	defaultMode, err := authsigning.APISignModeToInternal(encodingConfig.TxConfig.SignModeHandler().DefaultMode())
	s.Require().NoError(err)
	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	sigV2 := signing.SignatureV2{
		PubKey: priv.PubKey(),
		Data: &signing.SingleSignatureData{
			SignMode:  defaultMode,
			Signature: nil,
		},
		Sequence: seq,
	}

	sigsV2 := []signing.SignatureV2{sigV2}

	err = txBuilder.SetSignatures(sigsV2...)
	s.Require().NoError(err)

	// Second round: all signer infos are set, so each signer can sign.
	accNumber := s.app.AccountKeeper.GetAccount(s.ctx, accountAddress).GetAccountNumber()
	signerData := authsigning.SignerData{
		ChainID:       s.ctx.ChainID(),
		AccountNumber: accNumber,
		Sequence:      seq,
	}
	sigV2, err = tx.SignWithPrivKey(
		context.TODO(),
		defaultMode, signerData,
		txBuilder, priv, encodingConfig.TxConfig,
		seq,
	)
	s.Require().NoError(err)

	sigsV2 = []signing.SignatureV2{sigV2}
	err = txBuilder.SetSignatures(sigsV2...)
	s.Require().NoError(err)

	// bz are bytes to be broadcasted over the network
	bz, err := encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	s.Require().NoError(err)
	return bz
}

func deliverTx(tx []byte) *abci.ExecTxResult {
	txs := [][]byte{tx}
	height := s.app.LastBlockHeight() + 1
	res, err := s.app.FinalizeBlock(&abci.RequestFinalizeBlock{
		ProposerAddress: s.consAddress,
		Height:          height,
		Txs:             txs,
	})
	if err != nil {
		panic(err)
	}
	results := res.GetTxResults()
	if len(results) != 1 {
		panic("must have one result")
	}
	return results[0]
}
