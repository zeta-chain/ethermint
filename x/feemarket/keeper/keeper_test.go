package keeper_test

import (
	_ "embed"
	"math/big"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	stakingkeeper "github.com/cosmos/cosmos-sdk/x/staking/keeper"
	"github.com/evmos/ethermint/testutil"
	"github.com/stretchr/testify/suite"
)

type KeeperTestSuite struct {
	suite.Suite

	ctx         sdk.Context
	app         *app.EthermintApp
	queryClient types.QueryClient
	address     common.Address
	consAddress sdk.ConsAddress

	// for generate test tx
	clientCtx client.Context
	ethSigner ethtypes.Signer

	appCodec codec.Codec
	signer   keyring.Signer
	denom    string
}

var s *KeeperTestSuite

func TestKeeperTestSuite(t *testing.T) {
	s = new(KeeperTestSuite)
	suite.Run(t, s)

	// Run Ginkgo integration tests
	RegisterFailHandler(Fail)
	RunSpecs(t, "Keeper Suite")
}

// SetupTest setup test environment, it uses`require.TestingT` to support both `testing.T` and `testing.B`.
func (suite *KeeperTestSuite) SetupTest() {
	checkTx := false
	suite.app = app.Setup(checkTx, nil)
	suite.SetupApp(checkTx)
}

func (suite *KeeperTestSuite) SetupApp(checkTx bool) {
	t := suite.T()
	suite.SetupAccount(t)
	suite.SetupTestWithCb(t, nil)
	validator := suite.BaseTestSuiteWithAccount.PostSetupValidator(t)
	validator = stakingkeeper.TestingUpdateValidator(suite.App.StakingKeeper, suite.Ctx, validator, true)
	valBz, err := suite.App.StakingKeeper.ValidatorAddressCodec().StringToBytes(validator.GetOperator())
	suite.Require().NoError(err)
	err = suite.App.StakingKeeper.Hooks().AfterValidatorCreated(suite.Ctx, valBz)
	suite.Require().NoError(err)
	err = suite.App.StakingKeeper.SetValidatorByConsAddr(suite.Ctx, validator)
	suite.Require().NoError(err)
	suite.App.StakingKeeper.SetValidator(suite.Ctx, validator)
}

func (suite *KeeperTestSuite) TestSetGetBlockGasWanted() {
	testCases := []struct {
		name     string
		malleate func()
		expGas   uint64
	}{
		{
			"with last block given",
			func() {
				suite.app.FeeMarketKeeper.SetBlockGasWanted(suite.ctx, uint64(1000000))
			},
			uint64(1000000),
		},
	}
	for _, tc := range testCases {
		tc.malleate()

		gas := suite.app.FeeMarketKeeper.GetBlockGasWanted(suite.ctx)
		suite.Require().Equal(tc.expGas, gas, tc.name)
	}
}

func (suite *KeeperTestSuite) TestSetGetGasFee() {
	testCases := []struct {
		name     string
		malleate func()
		expFee   *big.Int
	}{
		{
			"with last block given",
			func() {
				suite.App.FeeMarketKeeper.SetBaseFee(suite.Ctx, sdkmath.LegacyOneDec().BigInt())
			},
			sdkmath.LegacyOneDec().BigInt(),
		},
	}

	for _, tc := range testCases {
		tc.malleate()

		fee := suite.app.FeeMarketKeeper.GetBaseFee(suite.ctx)
		suite.Require().Equal(tc.expFee, fee, tc.name)
	}
}
