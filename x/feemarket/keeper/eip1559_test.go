package keeper_test

import (
	"fmt"

	sdkmath "cosmossdk.io/math"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
)

func (suite *KeeperTestSuite) TestCalculateBaseFee() {
	testCases := []struct {
		name                 string
		NoBaseFee            bool
		blockHeight          int64
		parentBlockGasWanted uint64
		minGasPrice          sdkmath.LegacyDec
		expFee               func(baseFee sdkmath.Int) sdkmath.Int
	}{
		{
			"without BaseFee",
			true,
			0,
			0,
			sdkmath.LegacyZeroDec(),
			nil,
		},
		{
			"with BaseFee - initial EIP-1559 block",
			false,
			0,
			0,
			sdkmath.LegacyZeroDec(),
			func(baseFee sdkmath.Int) sdkmath.Int { return suite.app.FeeMarketKeeper.GetParams(suite.ctx).BaseFee },
		},
		{
			"with BaseFee - parent block wanted the same gas as its target (ElasticityMultiplier = 2)",
			false,
			1,
			50,
			sdkmath.LegacyZeroDec(),
			func(baseFee sdkmath.Int) sdkmath.Int { return suite.app.FeeMarketKeeper.GetParams(suite.ctx).BaseFee },
		},
		{
			"with BaseFee - parent block wanted the same gas as its target, with higher min gas price (ElasticityMultiplier = 2)",
			false,
			1,
			50,
			sdkmath.LegacyNewDec(1500000000),
			func(baseFee sdkmath.Int) sdkmath.Int { return suite.app.FeeMarketKeeper.GetParams(suite.ctx).BaseFee },
		},
		{
			"with BaseFee - parent block wanted more gas than its target (ElasticityMultiplier = 2)",
			false,
			1,
			100,
			sdkmath.LegacyZeroDec(),
			func(baseFee sdkmath.Int) sdkmath.Int { return baseFee.Add(sdkmath.NewInt(109375000)) },
		},
		{
			"with BaseFee - parent block wanted more gas than its target, with higher min gas price (ElasticityMultiplier = 2)",
			false,
			1,
			100,
			sdkmath.LegacyNewDec(1500000000),
			func(baseFee sdkmath.Int) sdkmath.Int { return baseFee.Add(sdkmath.NewInt(109375000)) },
		},
		{
			"with BaseFee - Parent gas wanted smaller than parent gas target (ElasticityMultiplier = 2)",
			false,
			1,
			25,
			sdkmath.LegacyZeroDec(),
			func(baseFee sdkmath.Int) sdkmath.Int { return baseFee.Sub(sdkmath.NewInt(54687500)) },
		},
		{
			"with BaseFee - Parent gas wanted smaller than parent gas target, with higher min gas price (ElasticityMultiplier = 2)",
			false,
			1,
			25,
			sdkmath.LegacyNewDec(1500000000),
			func(baseFee sdkmath.Int) sdkmath.Int { return sdkmath.NewInt(1500000000) },
		},
	}
	for _, tc := range testCases {
		suite.Run(fmt.Sprintf("Case %s", tc.name), func() {
			suite.SetupTest() // reset

			params := suite.app.FeeMarketKeeper.GetParams(suite.ctx)
			params.NoBaseFee = tc.NoBaseFee
			params.MinGasPrice = tc.minGasPrice

			err := suite.app.FeeMarketKeeper.SetParams(suite.ctx, params)
			suite.Require().NoError(err)
			// Set block height
			suite.ctx = suite.ctx.WithBlockHeight(tc.blockHeight)

			// Set parent block gas
			suite.app.FeeMarketKeeper.SetBlockGasWanted(suite.ctx, tc.parentBlockGasWanted)

			// Set next block target/gasLimit through Consensus Param MaxGas
			blockParams := tmproto.BlockParams{
				MaxGas:   100,
				MaxBytes: 10,
			}
			consParams := tmproto.ConsensusParams{Block: &blockParams}
			suite.ctx = suite.ctx.WithConsensusParams(consParams)

			// TODO: this is highly coupled with integration tests suite, fix test suites in general
			// fee := suite.app.FeeMarketKeeper.CalculateBaseFee(suite.ctx)
			// if tc.NoBaseFee {
			// 	suite.Require().Nil(fee, tc.name)
			// } else {
			// 	suite.Require().Equal(tc.expFee(params.BaseFee), sdkmath.NewIntFromBigInt(fee), tc.name)
			// }
		})
	}
}
