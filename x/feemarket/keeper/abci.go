// Copyright 2021 Evmos Foundation
// This file is part of Evmos' Ethermint library.
//
// The Ethermint library is free software: you can redistribute it and/or modify
// it under the terms of the GNU Lesser General Public License as published by
// the Free Software Foundation, either version 3 of the License, or
// (at your option) any later version.
//
// The Ethermint library is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
// GNU Lesser General Public License for more details.
//
// You should have received a copy of the GNU Lesser General Public License
// along with the Ethermint library. If not, see https://github.com/zeta-chain/ethermint/blob/main/LICENSE
package keeper

import (
	"errors"
	"fmt"
	"math"

	"github.com/zeta-chain/ethermint/x/feemarket/types"

	sdkmath "cosmossdk.io/math"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlock updates base fee
func (k *Keeper) BeginBlock(ctx sdk.Context) error {
	baseFee := k.CalculateBaseFee(ctx)

	// return immediately if base fee is nil
	if baseFee == nil {
		return nil
	}

	k.SetBaseFee(ctx, baseFee)

	defer func() {
		telemetry.SetGauge(float32(baseFee.Int64()), "feemarket", "base_fee")
	}()

	// Store current base fee in event
	ctx.EventManager().EmitEvents(sdk.Events{
		sdk.NewEvent(
			types.EventTypeFeeMarket,
			sdk.NewAttribute(types.AttributeKeyBaseFee, baseFee.String()),
		),
	})
	return nil
}

// EndBlock update block gas wanted.
// The EVM end block logic doesn't update the validator set, thus it returns
// an empty slice.
func (k *Keeper) EndBlock(ctx sdk.Context) error {
	if ctx.BlockGasMeter() == nil {
		return errors.New("block gas meter is nil when setting block gas wanted")
	}

	gasWanted := k.GetTransientGasWanted(ctx)
	gasUsed := ctx.BlockGasMeter().GasConsumedToLimit()

	if gasWanted > math.MaxInt64 {
		return fmt.Errorf("integer overflow by integer type conversion. Gas wanted %d > MaxInt64", gasWanted)
	}

	if gasUsed > math.MaxInt64 {
		return fmt.Errorf("integer overflow by integer type conversion. Gas used %d > MaxInt64", gasUsed)
	}

	// to prevent BaseFee manipulation we limit the gasWanted so that
	// gasWanted = max(gasWanted * MinGasMultiplier, gasUsed)
	// this will be keep BaseFee protected from un-penalized manipulation
	// more info here https://github.com/zeta-chain/ethermint/pull/1105#discussion_r888798925
	minGasMultiplier := k.GetParams(ctx).MinGasMultiplier
	limitedGasWanted := sdkmath.LegacyNewDec(int64(gasWanted)).Mul(minGasMultiplier)
	gasWanted = sdkmath.LegacyMaxDec(limitedGasWanted, sdkmath.LegacyNewDec(int64(gasUsed))).TruncateInt().Uint64()
	k.SetBlockGasWanted(ctx, gasWanted)

	defer func() {
		telemetry.SetGauge(float32(gasWanted), "feemarket", "block_gas")
	}()

	ctx.EventManager().EmitEvent(sdk.NewEvent(
		"block_gas",
		sdk.NewAttribute("height", fmt.Sprintf("%d", ctx.BlockHeight())),
		sdk.NewAttribute("amount", fmt.Sprintf("%d", gasWanted)),
	))
	return nil
}
