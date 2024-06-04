package statedb_test

import (
	"errors"
	"math/big"
	"testing"

	consensusparamkeeper "github.com/cosmos/cosmos-sdk/x/consensus/keeper"
	consensusparamtypes "github.com/cosmos/cosmos-sdk/x/consensus/types"

	dbm "github.com/cometbft/cometbft-db"
	"github.com/cometbft/cometbft/libs/log"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	capabilitytypes "github.com/cosmos/cosmos-sdk/x/capability/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	paramstypes "github.com/cosmos/cosmos-sdk/x/params/types"
	"github.com/ethereum/go-ethereum/common"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/ethereum/go-ethereum/core/vm"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/evmos/ethermint/app"
	"github.com/evmos/ethermint/encoding"
	ethermint "github.com/evmos/ethermint/types"
	evmkeeper "github.com/evmos/ethermint/x/evm/keeper"
	"github.com/evmos/ethermint/x/evm/statedb"
	evmtypes "github.com/evmos/ethermint/x/evm/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

var (
	address       common.Address   = common.BigToAddress(big.NewInt(101))
	address2      common.Address   = common.BigToAddress(big.NewInt(102))
	address3      common.Address   = common.BigToAddress(big.NewInt(103))
	blockHash     common.Hash      = common.BigToHash(big.NewInt(0))
	emptyTxConfig statedb.TxConfig = statedb.NewEmptyTxConfig(blockHash)
)

type StateDBTestSuite struct {
	suite.Suite
}

func (suite *StateDBTestSuite) TestAccount() {
	key1 := common.BigToHash(big.NewInt(1))
	value1 := common.BigToHash(big.NewInt(2))
	key2 := common.BigToHash(big.NewInt(3))
	value2 := common.BigToHash(big.NewInt(4))
	txConfig := emptyTxConfig
	txConfig.TxHash = common.BigToHash(big.NewInt(100))
	testCases := []struct {
		name     string
		malleate func(*statedb.StateDB, sdk.MultiStore)
	}{
		{"non-exist account", func(db *statedb.StateDB, cms sdk.MultiStore) {
			suite.Require().Equal(false, db.Exist(address))
			suite.Require().Equal(true, db.Empty(address))
			suite.Require().Equal(big.NewInt(0), db.GetBalance(address))
			suite.Require().Equal([]byte(nil), db.GetCode(address))
			suite.Require().Equal(common.Hash{}, db.GetCodeHash(address))
			suite.Require().Equal(uint64(0), db.GetNonce(address))
		}},
		{"empty account", func(db *statedb.StateDB, cms sdk.MultiStore) {
			db.CreateAccount(address)
			suite.Require().NoError(db.Commit())

			ctx, keeper := newTestKeeper(suite.T(), cms)
			acct := keeper.GetAccount(ctx, address)
			suite.Require().Equal(statedb.NewEmptyAccount(), acct)
			suite.Require().False(acct.IsContract())

			db = statedb.New(ctx, keeper, txConfig)
			suite.Require().Equal(true, db.Exist(address))
			suite.Require().Equal(true, db.Empty(address))
			suite.Require().Equal(big.NewInt(0), db.GetBalance(address))
			suite.Require().Equal([]byte(nil), db.GetCode(address))
			suite.Require().Equal(common.BytesToHash(emptyCodeHash), db.GetCodeHash(address))
			suite.Require().Equal(uint64(0), db.GetNonce(address))
		}},
		{"suicide", func(db *statedb.StateDB, cms sdk.MultiStore) {
			// non-exist account.
			suite.Require().False(db.Suicide(address))
			suite.Require().False(db.HasSuicided(address))

			// create a contract account
			db.CreateAccount(address)
			db.SetCode(address, []byte("hello world"))
			db.AddBalance(address, big.NewInt(100))
			db.SetState(address, key1, value1)
			db.SetState(address, key2, value2)
			codeHash := db.GetCodeHash(address)
			suite.Require().NoError(db.Commit())

			ctx, keeper := newTestKeeper(suite.T(), cms)

			suite.Require().NotEmpty(keeper.GetCode(ctx, codeHash))

			// suicide
			db = statedb.New(ctx, keeper, txConfig)
			suite.Require().False(db.HasSuicided(address))
			suite.Require().True(db.Suicide(address))

			// check dirty state
			suite.Require().True(db.HasSuicided(address))
			// balance is cleared
			suite.Require().Equal(big.NewInt(0), db.GetBalance(address))
			// but code and state are still accessible in dirty state
			suite.Require().Equal(value1, db.GetState(address, key1))
			suite.Require().Equal([]byte("hello world"), db.GetCode(address))

			suite.Require().NoError(db.Commit())

			ctx, keeper = newTestKeeper(suite.T(), cms)

			// not accessible from StateDB anymore
			db = statedb.New(ctx, keeper, txConfig)
			suite.Require().False(db.Exist(address))

			// and cleared in keeper too
			suite.Require().Nil(keeper.GetAccount(ctx, address))
			// code is not deleted when contract suicided.
			suite.Require().NotEmpty(keeper.GetCode(ctx, codeHash))
		}},
	}
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			raw, ctx, keeper := setupTestEnv(suite.T())
			db := statedb.New(ctx, keeper, txConfig)
			tc.malleate(db, raw)
		})
	}
}

func (suite *StateDBTestSuite) TestAccountOverride() {
	_, ctx, keeper := setupTestEnv(suite.T())
	db := statedb.New(ctx, keeper, emptyTxConfig)
	// test balance carry over when overwritten
	amount := big.NewInt(1)

	// init an EOA account, account overriden only happens on EOA account.
	db.AddBalance(address, amount)
	db.SetNonce(address, 1)

	// override
	db.CreateAccount(address)

	// check balance is not lost
	suite.Require().Equal(amount, db.GetBalance(address))
	// but nonce is reset
	suite.Require().Equal(uint64(0), db.GetNonce(address))
}

func (suite *StateDBTestSuite) TestDBError() {
	testCases := []struct {
		name     string
		malleate func(vm.StateDB)
	}{
		{"negative balance", func(db vm.StateDB) {
			db.SubBalance(address, big.NewInt(10))
		}},
	}
	for _, tc := range testCases {
		_, ctx, keeper := setupTestEnv(suite.T())
		db := statedb.New(ctx, keeper, emptyTxConfig)
		tc.malleate(db)
		suite.Require().Error(db.Commit())
	}
}

func (suite *StateDBTestSuite) TestBalance() {
	// NOTE: no need to test overflow/underflow, that is guaranteed by evm implementation.
	testCases := []struct {
		name       string
		malleate   func(*statedb.StateDB, sdk.MultiStore)
		expBalance *big.Int
	}{
		{"add balance", func(db *statedb.StateDB, cms sdk.MultiStore) {
			db.AddBalance(address, big.NewInt(10))
		}, big.NewInt(10)},
		{"sub balance", func(db *statedb.StateDB, cms sdk.MultiStore) {
			db.AddBalance(address, big.NewInt(10))
			// get dirty balance
			suite.Require().Equal(big.NewInt(10), db.GetBalance(address))
			db.SubBalance(address, big.NewInt(2))
		}, big.NewInt(8)},
		{"add zero balance", func(db *statedb.StateDB, cms sdk.MultiStore) {
			db.AddBalance(address, big.NewInt(0))
		}, big.NewInt(0)},
		{"sub zero balance", func(db *statedb.StateDB, cms sdk.MultiStore) {
			db.SubBalance(address, big.NewInt(0))
		}, big.NewInt(0)},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			raw, ctx, keeper := setupTestEnv(suite.T())
			db := statedb.New(ctx, keeper, emptyTxConfig)
			tc.malleate(db, raw)

			// check dirty state
			suite.Require().Equal(tc.expBalance, db.GetBalance(address))
			suite.Require().NoError(db.Commit())
			// check committed balance too
			suite.Require().Equal(tc.expBalance, keeper.GetBalance(ctx, address))
		})
	}
}

func (suite *StateDBTestSuite) TestState() {
	key1 := common.BigToHash(big.NewInt(1))
	value1 := common.BigToHash(big.NewInt(1))
	testCases := []struct {
		name      string
		malleate  func(*statedb.StateDB)
		expStates statedb.Storage
	}{
		{"empty state", func(db *statedb.StateDB) {
		}, nil},
		{"set empty value", func(db *statedb.StateDB) {
			db.SetState(address, key1, common.Hash{})
		}, statedb.Storage{}},
		{"noop state change", func(db *statedb.StateDB) {
			db.SetState(address, key1, value1)
			db.SetState(address, key1, common.Hash{})
		}, statedb.Storage{}},
		{"set state", func(db *statedb.StateDB) {
			// check empty initial state
			suite.Require().Equal(common.Hash{}, db.GetState(address, key1))
			suite.Require().Equal(common.Hash{}, db.GetCommittedState(address, key1))

			// set state
			db.SetState(address, key1, value1)
			// query dirty state
			suite.Require().Equal(value1, db.GetState(address, key1))
			// check committed state is still not exist
			suite.Require().Equal(common.Hash{}, db.GetCommittedState(address, key1))

			// set same value again, should be noop
			db.SetState(address, key1, value1)
			suite.Require().Equal(value1, db.GetState(address, key1))
		}, statedb.Storage{
			key1: value1,
		}},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			raw, ctx, keeper := setupTestEnv(suite.T())
			db := statedb.New(ctx, keeper, emptyTxConfig)
			tc.malleate(db)
			suite.Require().NoError(db.Commit())

			// check committed states in keeper
			ctx, keeper = newTestKeeper(suite.T(), raw)
			for k, v := range tc.expStates {
				suite.Require().Equal(v, keeper.GetState(ctx, address, k))
			}

			// check ForEachStorage
			db = statedb.New(ctx, keeper, emptyTxConfig)
			collected := CollectContractStorage(db)
			if len(tc.expStates) > 0 {
				suite.Require().Equal(tc.expStates, collected)
			} else {
				suite.Require().Empty(collected)
			}
		})
	}
}

func (suite *StateDBTestSuite) TestCode() {
	code := []byte("hello world")
	codeHash := crypto.Keccak256Hash(code)

	testCases := []struct {
		name        string
		malleate    func(vm.StateDB)
		expCode     []byte
		expCodeHash common.Hash
	}{
		{"non-exist account", func(vm.StateDB) {}, nil, common.Hash{}},
		{"empty account", func(db vm.StateDB) {
			db.CreateAccount(address)
		}, nil, common.BytesToHash(emptyCodeHash)},
		{"set code", func(db vm.StateDB) {
			db.SetCode(address, code)
		}, code, codeHash},
	}

	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			raw, ctx, keeper := setupTestEnv(suite.T())
			db := statedb.New(ctx, keeper, emptyTxConfig)
			tc.malleate(db)

			// check dirty state
			suite.Require().Equal(tc.expCode, db.GetCode(address))
			suite.Require().Equal(len(tc.expCode), db.GetCodeSize(address))
			suite.Require().Equal(tc.expCodeHash, db.GetCodeHash(address))

			suite.Require().NoError(db.Commit())

			// check again
			//db = statedb.New(sdk.Context{}, keeper, emptyTxConfig)
			//suite.Require().Equal(tc.expCode, db.GetCode(address))
			//suite.Require().Equal(len(tc.expCode), db.GetCodeSize(address))
			//suite.Require().Equal(tc.expCodeHash, db.GetCodeHash(address))
			ctx, keeper = newTestKeeper(suite.T(), raw)
			db = statedb.New(ctx, keeper, emptyTxConfig)
			suite.Require().Equal(tc.expCode, db.GetCode(address))
			suite.Require().Equal(len(tc.expCode), db.GetCodeSize(address))
			suite.Require().Equal(tc.expCodeHash, db.GetCodeHash(address))
		})
	}
}

func (suite *StateDBTestSuite) TestRevertSnapshot() {
	v1 := common.BigToHash(big.NewInt(1))
	v2 := common.BigToHash(big.NewInt(2))
	v3 := common.BigToHash(big.NewInt(3))
	testCases := []struct {
		name     string
		malleate func(vm.StateDB)
	}{
		{"set state", func(db vm.StateDB) {
			db.SetState(address, v1, v3)
		}},
		{"set nonce", func(db vm.StateDB) {
			db.SetNonce(address, 10)
		}},
		{"change balance", func(db vm.StateDB) {
			db.AddBalance(address, big.NewInt(10))
			db.SubBalance(address, big.NewInt(5))
		}},
		{"override account", func(db vm.StateDB) {
			db.CreateAccount(address)
		}},
		{"set code", func(db vm.StateDB) {
			db.SetCode(address, []byte("hello world"))
		}},
		{"suicide", func(db vm.StateDB) {
			db.SetState(address, v1, v2)
			db.SetCode(address, []byte("hello world"))
			suite.Require().True(db.Suicide(address))
		}},
		{"add log", func(db vm.StateDB) {
			db.AddLog(&ethtypes.Log{
				Address: address,
			})
		}},
		{"add refund", func(db vm.StateDB) {
			db.AddRefund(10)
			db.SubRefund(5)
		}},
		{"access list", func(db vm.StateDB) {
			db.AddAddressToAccessList(address)
			db.AddSlotToAccessList(address, v1)
		}},
	}
	for _, tc := range testCases {
		suite.Run(tc.name, func() {
			raw, ctx, keeper := setupTestEnv(suite.T())
			ctx = ctx.WithEventManager(sdk.NewEventManager())

			{
				// do some arbitrary changes to the storage
				db := statedb.New(ctx, keeper, emptyTxConfig)
				db.SetNonce(address, 1)
				db.AddBalance(address, big.NewInt(100))
				db.SetCode(address, []byte("hello world"))
				db.SetState(address, v1, v2)
				db.SetNonce(address2, 1)
				suite.Require().NoError(db.Commit())
			}

			originalState := cloneRawState(suite.T(), raw)
			ctx, keeper = newTestKeeper(suite.T(), raw)

			// run test
			db := statedb.New(ctx, keeper, emptyTxConfig)
			rev := db.Snapshot()
			tc.malleate(db)
			db.RevertToSnapshot(rev)

			// check empty states after revert
			suite.Require().Zero(db.GetRefund())
			suite.Require().Empty(db.Logs())

			suite.Require().NoError(db.Commit())

			newState := cloneRawState(suite.T(), raw)
			// check the committed state stays the same
			suite.Require().Equal(originalState, newState)
		})
	}
}

func (suite *StateDBTestSuite) TestNestedSnapshot() {
	key := common.BigToHash(big.NewInt(1))
	value1 := common.BigToHash(big.NewInt(1))
	value2 := common.BigToHash(big.NewInt(2))

	_, ctx, keeper := setupTestEnv(suite.T())
	db := statedb.New(ctx, keeper, emptyTxConfig)

	rev1 := db.Snapshot()
	db.SetState(address, key, value1)

	rev2 := db.Snapshot()
	db.SetState(address, key, value2)
	suite.Require().Equal(value2, db.GetState(address, key))

	db.RevertToSnapshot(rev2)
	suite.Require().Equal(value1, db.GetState(address, key))

	db.RevertToSnapshot(rev1)
	suite.Require().Equal(common.Hash{}, db.GetState(address, key))
}

func (suite *StateDBTestSuite) TestInvalidSnapshotId() {
	_, ctx, keeper := setupTestEnv(suite.T())
	db := statedb.New(ctx, keeper, emptyTxConfig)
	suite.Require().Panics(func() {
		db.RevertToSnapshot(1)
	})
}

func (suite *StateDBTestSuite) TestAccessList() {
	value1 := common.BigToHash(big.NewInt(1))
	value2 := common.BigToHash(big.NewInt(2))

	testCases := []struct {
		name     string
		malleate func(vm.StateDB)
	}{
		{"add address", func(db vm.StateDB) {
			suite.Require().False(db.AddressInAccessList(address))
			db.AddAddressToAccessList(address)
			suite.Require().True(db.AddressInAccessList(address))

			addrPresent, slotPresent := db.SlotInAccessList(address, value1)
			suite.Require().True(addrPresent)
			suite.Require().False(slotPresent)

			// add again, should be no-op
			db.AddAddressToAccessList(address)
			suite.Require().True(db.AddressInAccessList(address))
		}},
		{"add slot", func(db vm.StateDB) {
			addrPresent, slotPresent := db.SlotInAccessList(address, value1)
			suite.Require().False(addrPresent)
			suite.Require().False(slotPresent)
			db.AddSlotToAccessList(address, value1)
			addrPresent, slotPresent = db.SlotInAccessList(address, value1)
			suite.Require().True(addrPresent)
			suite.Require().True(slotPresent)

			// add another slot
			db.AddSlotToAccessList(address, value2)
			addrPresent, slotPresent = db.SlotInAccessList(address, value2)
			suite.Require().True(addrPresent)
			suite.Require().True(slotPresent)

			// add again, should be noop
			db.AddSlotToAccessList(address, value2)
			addrPresent, slotPresent = db.SlotInAccessList(address, value2)
			suite.Require().True(addrPresent)
			suite.Require().True(slotPresent)
		}},
		{"prepare access list", func(db vm.StateDB) {
			al := ethtypes.AccessList{{
				Address:     address3,
				StorageKeys: []common.Hash{value1},
			}}
			db.PrepareAccessList(address, &address2, vm.PrecompiledAddressesBerlin, al)

			// check sender and dst
			suite.Require().True(db.AddressInAccessList(address))
			suite.Require().True(db.AddressInAccessList(address2))
			// check precompiles
			suite.Require().True(db.AddressInAccessList(common.BytesToAddress([]byte{1})))
			// check AccessList
			suite.Require().True(db.AddressInAccessList(address3))
			addrPresent, slotPresent := db.SlotInAccessList(address3, value1)
			suite.Require().True(addrPresent)
			suite.Require().True(slotPresent)
			addrPresent, slotPresent = db.SlotInAccessList(address3, value2)
			suite.Require().True(addrPresent)
			suite.Require().False(slotPresent)
		}},
	}

	for _, tc := range testCases {
		_, ctx, keeper := setupTestEnv(suite.T())
		db := statedb.New(ctx, keeper, emptyTxConfig)
		tc.malleate(db)
	}
}

func (suite *StateDBTestSuite) TestLog() {
	txHash := common.BytesToHash([]byte("tx"))
	// use a non-default tx config
	txConfig := statedb.NewTxConfig(
		blockHash,
		txHash,
		1, 1,
	)
	_, ctx, keeper := setupTestEnv(suite.T())
	db := statedb.New(ctx, keeper, txConfig)
	data := []byte("hello world")
	db.AddLog(&ethtypes.Log{
		Address:     address,
		Topics:      []common.Hash{},
		Data:        data,
		BlockNumber: 1,
	})
	suite.Require().Equal(1, len(db.Logs()))
	expecedLog := &ethtypes.Log{
		Address:     address,
		Topics:      []common.Hash{},
		Data:        data,
		BlockNumber: 1,
		BlockHash:   blockHash,
		TxHash:      txHash,
		TxIndex:     1,
		Index:       1,
	}
	suite.Require().Equal(expecedLog, db.Logs()[0])

	db.AddLog(&ethtypes.Log{
		Address:     address,
		Topics:      []common.Hash{},
		Data:        data,
		BlockNumber: 1,
	})
	suite.Require().Equal(2, len(db.Logs()))
	expecedLog.Index++
	suite.Require().Equal(expecedLog, db.Logs()[1])
}

func (suite *StateDBTestSuite) TestRefund() {
	testCases := []struct {
		name      string
		malleate  func(vm.StateDB)
		expRefund uint64
		expPanic  bool
	}{
		{"add refund", func(db vm.StateDB) {
			db.AddRefund(uint64(10))
		}, 10, false},
		{"sub refund", func(db vm.StateDB) {
			db.AddRefund(uint64(10))
			db.SubRefund(uint64(5))
		}, 5, false},
		{"negative refund counter", func(db vm.StateDB) {
			db.AddRefund(uint64(5))
			db.SubRefund(uint64(10))
		}, 0, true},
	}
	for _, tc := range testCases {
		_, ctx, keeper := setupTestEnv(suite.T())
		db := statedb.New(ctx, keeper, emptyTxConfig)
		if !tc.expPanic {
			tc.malleate(db)
			suite.Require().Equal(tc.expRefund, db.GetRefund())
		} else {
			suite.Require().Panics(func() {
				tc.malleate(db)
			})
		}
	}
}

func (suite *StateDBTestSuite) TestIterateStorage() {
	key1 := common.BigToHash(big.NewInt(1))
	value1 := common.BigToHash(big.NewInt(2))
	key2 := common.BigToHash(big.NewInt(3))
	value2 := common.BigToHash(big.NewInt(4))

	raw, ctx, keeper := setupTestEnv(suite.T())
	db := statedb.New(ctx, keeper, emptyTxConfig)
	db.SetState(address, key1, value1)
	db.SetState(address, key2, value2)

	// ForEachStorage only iterate committed state
	suite.Require().Empty(CollectContractStorage(db))

	suite.Require().NoError(db.Commit())

	storage := CollectContractStorage(db)
	suite.Require().Equal(2, len(storage))

	ctx, keeper = newTestKeeper(suite.T(), raw)
	for k, v := range storage {
		suite.Require().Equal(v, keeper.GetState(ctx, address, k))
	}

	// break early iteration
	storage = make(statedb.Storage)
	db.ForEachStorage(address, func(k, v common.Hash) bool {
		storage[k] = v
		// return false to break early
		return false
	})
	suite.Require().Equal(1, len(storage))
}

func (suite *StateDBTestSuite) TestNativeAction() {
	_, ctx, keeper := setupTestEnv(suite.T())
	storeKey := testStoreKeys["testnative"]
	transientKey := testTransientKeys[evmtypes.TransientKey]
	memKey := testMemKeys[capabilitytypes.MemStoreKey]

	eventConverter := func(event sdk.Event) (*ethtypes.Log, error) {
		converters := map[string]statedb.EventConverter{
			"success1": func(event sdk.Event) (*ethtypes.Log, error) {
				return &ethtypes.Log{Data: []byte("success1")}, nil
			},
			"success2": func(event sdk.Event) (*ethtypes.Log, error) {
				return &ethtypes.Log{Data: []byte("success2")}, nil
			},
		}
		converter, ok := converters[event.Type]
		if !ok {
			return nil, nil
		}
		return converter(event)
	}

	stateDB := statedb.New(ctx, keeper, emptyTxConfig)
	contract := common.BigToAddress(big.NewInt(101))

	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		store.Set([]byte("success1"), []byte("value"))
		ctx.EventManager().EmitEvent(sdk.NewEvent("success1"))

		transient := ctx.KVStore(transientKey)
		transient.Set([]byte("transient"), []byte("value"))

		mem := ctx.KVStore(memKey)
		mem.Set([]byte("mem"), []byte("value"))

		return nil
	})
	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		store.Set([]byte("failure1"), []byte("value"))
		ctx.EventManager().EmitEvent(sdk.NewEvent("failure1"))

		transient := ctx.KVStore(transientKey)
		suite.Require().Equal([]byte("value"), transient.Get([]byte("transient")))

		mem := ctx.KVStore(memKey)
		suite.Require().Equal([]byte("value"), mem.Get([]byte("mem")))
		return errors.New("failure")
	})

	// check events
	suite.Require().Equal(sdk.Events{{Type: "success1"}}, stateDB.NativeEvents())
	suite.Require().Equal([]*ethtypes.Log{{
		Address: contract,
		Data:    []byte("success1"),
	}}, stateDB.Logs())

	// test query
	stateDB.ExecuteNativeAction(contract, nil, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		suite.Require().Equal([]byte("value"), store.Get([]byte("success1")))
		suite.Require().Nil(store.Get([]byte("failure1")))
		return nil
	})

	rev1 := stateDB.Snapshot()
	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		store.Set([]byte("success2"), []byte("value"))
		ctx.EventManager().EmitEvent(sdk.NewEvent("success2"))
		return nil
	})
	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		store.Set([]byte("failure2"), []byte("value"))
		ctx.EventManager().EmitEvent(sdk.NewEvent("failure2"))
		return errors.New("failure")
	})

	// check events
	suite.Require().Equal(sdk.Events{{Type: "success1"}, {Type: "success2"}}, stateDB.NativeEvents())

	suite.Require().Equal([]*ethtypes.Log{{
		Address: contract,
		Data:    []byte("success1"),
	}, {
		Index:   1,
		Address: contract,
		Data:    []byte("success2"),
	}}, stateDB.Logs())

	// test query
	stateDB.ExecuteNativeAction(contract, nil, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		suite.Require().Equal([]byte("value"), store.Get([]byte("success1")))
		suite.Require().Equal([]byte("value"), store.Get([]byte("success2")))
		suite.Require().Nil(store.Get([]byte("failure2")))
		return nil
	})

	stateDB.RevertToSnapshot(rev1)

	// check events
	suite.Require().Equal(sdk.Events{{Type: "success1"}}, stateDB.NativeEvents())
	suite.Require().Equal([]*ethtypes.Log{{
		Address: contract,
		Data:    []byte("success1"),
	}}, stateDB.Logs())

	_ = stateDB.Snapshot()
	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		store.Set([]byte("success3"), []byte("value"))
		ctx.EventManager().EmitEvent(sdk.NewEvent("success3"))
		return nil
	})

	// check events
	suite.Require().Equal(sdk.Events{{Type: "success1"}, {Type: "success3"}}, stateDB.NativeEvents())

	// test query
	stateDB.ExecuteNativeAction(contract, eventConverter, func(ctx sdk.Context) error {
		store := ctx.KVStore(storeKey)
		suite.Require().Equal([]byte("value"), store.Get([]byte("success1")))
		suite.Require().Nil(store.Get([]byte("success2")))
		suite.Require().Equal([]byte("value"), store.Get([]byte("success3")))
		return nil
	})

	suite.Require().NoError(stateDB.Commit())

	// query committed state
	store := ctx.KVStore(storeKey)
	suite.Require().Equal([]byte("value"), store.Get([]byte("success1")))
	suite.Require().Nil(store.Get([]byte("success2")))
	suite.Require().Equal([]byte("value"), store.Get([]byte("success3")))
	suite.Require().Nil(store.Get([]byte("failure1")))
	suite.Require().Nil(store.Get([]byte("failure2")))

	// check events
	suite.Require().Equal(sdk.Events{{Type: "success1"}, {Type: "success3"}}, ctx.EventManager().Events())
}

func CollectContractStorage(db vm.StateDB) statedb.Storage {
	storage := make(statedb.Storage)
	db.ForEachStorage(address, func(k, v common.Hash) bool {
		storage[k] = v
		return true
	})
	return storage
}

var (
	testStoreKeys     = sdk.NewKVStoreKeys(authtypes.StoreKey, banktypes.StoreKey, evmtypes.StoreKey, "testnative")
	testTransientKeys = sdk.NewTransientStoreKeys(evmtypes.TransientKey)
	testMemKeys       = sdk.NewMemoryStoreKeys(capabilitytypes.MemStoreKey)
)

func cloneRawState(t *testing.T, cms sdk.MultiStore) map[string]map[string][]byte {
	result := make(map[string]map[string][]byte)

	for name, key := range testStoreKeys {
		store := cms.GetKVStore(key)
		itr := store.Iterator(nil, nil)
		defer itr.Close()

		state := make(map[string][]byte)
		for ; itr.Valid(); itr.Next() {
			state[string(itr.Key())] = itr.Value()
		}

		result[name] = state
	}

	return result
}

func newTestKeeper(t *testing.T, cms sdk.MultiStore) (sdk.Context, *evmkeeper.Keeper) {
	appCodec := encoding.MakeConfig(app.ModuleBasics).Codec
	authAddr := authtypes.NewModuleAddress(govtypes.ModuleName).String()
	accountKeeper := authkeeper.NewAccountKeeper(
		appCodec, testStoreKeys[authtypes.StoreKey],
		ethermint.ProtoAccount,
		map[string][]string{
			evmtypes.ModuleName: {authtypes.Minter, authtypes.Burner},
		},
		sdk.GetConfig().GetBech32AccountAddrPrefix(),
		authAddr,
	)
	bankKeeper := bankkeeper.NewBaseKeeper(
		appCodec,
		testStoreKeys[banktypes.StoreKey],
		accountKeeper,
		map[string]bool{},
		authAddr,
	)
	conKeeper := consensusparamkeeper.NewKeeper(appCodec, testStoreKeys[consensusparamtypes.StoreKey], authAddr)

	allKeys := make(map[string]storetypes.StoreKey, len(testStoreKeys)+len(testTransientKeys)+len(testMemKeys))
	for k, v := range testStoreKeys {
		allKeys[k] = v
	}
	for k, v := range testTransientKeys {
		allKeys[k] = v
	}
	for k, v := range testMemKeys {
		allKeys[k] = v
	}
	evmKeeper := evmkeeper.NewKeeper(
		appCodec, testStoreKeys[evmtypes.StoreKey], testTransientKeys[evmtypes.TransientKey], authtypes.NewModuleAddress(govtypes.ModuleName),
		accountKeeper, bankKeeper, nil, nil,
		"",
		paramstypes.Subspace{}, nil, conKeeper,
		allKeys,
	)

	ctx := sdk.NewContext(cms, tmproto.Header{}, false, log.NewNopLogger())
	return ctx, evmKeeper
}

func setupTestEnv(t *testing.T) (sdk.MultiStore, sdk.Context, *evmkeeper.Keeper) {
	db := dbm.NewMemDB()
	cms := rootmulti.NewStore(db, log.NewNopLogger())
	for _, key := range testStoreKeys {
		cms.MountStoreWithDB(key, storetypes.StoreTypeIAVL, nil)
	}
	for _, key := range testTransientKeys {
		cms.MountStoreWithDB(key, storetypes.StoreTypeTransient, nil)
	}
	for _, key := range testMemKeys {
		cms.MountStoreWithDB(key, storetypes.StoreTypeMemory, nil)
	}
	require.NoError(t, cms.LoadLatestVersion())

	ctx, keeper := newTestKeeper(t, cms)
	require.NoError(t, keeper.SetParams(ctx, evmtypes.Params{
		EvmDenom: "uphoton",
	}))
	return cms, ctx, keeper
}

func TestStateDBTestSuite(t *testing.T) {
	suite.Run(t, &StateDBTestSuite{})
}
