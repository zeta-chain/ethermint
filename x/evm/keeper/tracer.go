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
	"math/big"
	"os"

	"github.com/ethereum/go-ethereum/core"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/eth/tracers/logger"
	"github.com/ethereum/go-ethereum/params"
	"github.com/zeta-chain/ethermint/x/evm/types"
)

// NewTracer creates a new Logger tracer to collect execution traces from an
// EVM transaction.
func NewTracer(tracer string, msg *core.Message, cfg *params.ChainConfig, height int64) vm.EVMLogger {
	// TODO: enable additional log configuration
	logCfg := &logger.Config{
		Debug: true,
	}

	switch tracer {
	case types.TracerAccessList:
		preCompiles := vm.ActivePrecompiles(cfg.Rules(big.NewInt(height), cfg.MergeNetsplitBlock != nil, 1))
		return logger.NewAccessListTracer(msg.AccessList, msg.From, *msg.To, preCompiles)
	case types.TracerJSON:
		return logger.NewJSONLogger(logCfg, os.Stderr)
	case types.TracerMarkdown:
		return logger.NewMarkdownLogger(logCfg, os.Stdout) // TODO: Stderr ?
	case types.TracerStruct:
		return logger.NewStructLogger(logCfg)
	default:
		return types.NewNoOpTracer()
	}
}
