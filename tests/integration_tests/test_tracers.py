
from web3 import Web3

from .expected_constants import (
    EXPECTED_CALLTRACERS,
    EXPECTED_CONTRACT_CREATE_TRACER,
    EXPECTED_STRUCT_TRACER,
)
from .utils import (
    ADDRS,
    CONTRACTS,
    KEYS,
    deploy_contract,
    send_transaction,
    w3_wait_for_new_blocks,
)


def test_tracers(ethermint_rpc_ws):
    w3: Web3 = ethermint_rpc_ws.w3
    eth_rpc = w3.provider
    gas_price = w3.eth.gas_price
    tx = {"to": ADDRS["community"], "value": 100, "gasPrice": gas_price}
    tx_hash = send_transaction(w3, tx, KEYS["validator"])["transactionHash"].hex()

    tx_res = eth_rpc.make_request("debug_traceTransaction", [tx_hash])
    assert tx_res["result"] == EXPECTED_STRUCT_TRACER, ""

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == EXPECTED_CONTRACT_CREATE_TRACER, res

    tx_res = eth_rpc.make_request(
        "debug_traceTransaction",
        [tx_hash, {"tracer": "callTracer", "tracerConfig": "{'onlyTopCall':True}"}],
    )
    assert tx_res["result"] == EXPECTED_CALLTRACERS, ""

    _, tx = deploy_contract(
        w3,
        CONTRACTS["TestERC20A"],
    )
    tx_hash = tx["transactionHash"].hex()

    w3_wait_for_new_blocks(w3, 1)

def test_trace_tx(ethermint, geth):
    method = "debug_traceTransaction"
    tracer = {"tracer": "callTracer"}
    tracers = [
        [],
        [tracer],
        [tracer | {"tracerConfig": {"onlyTopCall": True}}],
        [tracer | {"tracerConfig": {"withLog": True}}],
        [tracer | {"tracerConfig": {"diffMode": True}}],
    ]
    iterations = 1
    acc = derive_random_account()

    def process(w3):
        # fund new sender to deploy contract with same address
        fund_acc(w3, acc)
        contract, _ = deploy_contract(w3, CONTRACTS["TestMessageCall"], key=acc.key)
        tx = contract.functions.test(iterations).build_transaction()
        tx_hash = send_transaction(w3, tx)["transactionHash"].hex()
        res = []
        call = w3.provider.make_request
        with ThreadPoolExecutor(len(tracers)) as exec:
            params = [([tx_hash] + cfg) for cfg in tracers]
            exec_map = exec.map(call, itertools.repeat(method), params)
            res = [json.dumps(resp["result"], sort_keys=True) for resp in exec_map]
        return res

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1], res


def test_tracecall_insufficient_funds(ethermint, geth):
    method = "debug_traceCall"
    acc = derive_random_account()
    sender = acc.address
    receiver = ADDRS["community"]
    value = hex(100)
    gas = hex(21000)

    def process(w3):
        fund_acc(w3, acc)
        # Insufficient funds
        tx = {
            # an non-exist address
            "from": "0x1000000000000000000000000000000000000000",
            "to": receiver,
            "value": value,
            "gasPrice": hex(w3.eth.gas_price),
            "gas": gas,
        }
        call = w3.provider.make_request
        tracers = ["prestateTracer", "callTracer"]
        with ThreadPoolExecutor(len(tracers)) as exec:
            params = [([tx, "latest", {"tracer": tracer}]) for tracer in tracers]
            for resp in exec.map(call, itertools.repeat(method), params):
                assert "error" in resp
                assert "insufficient" in resp["error"]["message"], resp["error"]

        tx = {"from": sender, "to": receiver, "value": value, "gas": gas}
        tracer = {"tracer": "callTracer"}
        tracers = [
            [],
            [tracer],
            [tracer | {"tracerConfig": {"onlyTopCall": True}}],
        ]
        res = []
        with ThreadPoolExecutor(len(tracers)) as exec:
            params = [([tx, "latest"] + cfg) for cfg in tracers]
            exec_map = exec.map(call, itertools.repeat(method), params)
            res = [json.dumps(resp["result"], sort_keys=True) for resp in exec_map]
        return res

    providers = [ethermint.w3, geth.w3]
    expected = json.dumps(EXPECTED_CALLTRACERS | {"from": sender.lower()})
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert (
            res[0]
            == res[-1]
            == [
                json.dumps(EXPECTED_STRUCT_TRACER),
                expected,
                expected,
            ]
        ), res


def test_js_tracers(ethermint, geth):
    method = "debug_traceCall"
    acc = derive_new_account(n=2)
    sender = acc.address

    def process(w3):
        # fund new sender to deploy contract with same address
        fund_acc(w3, acc)
        contract, _ = deploy_contract(w3, CONTRACTS["Greeter"], key=acc.key)
        tx = contract.functions.setGreeting("world").build_transaction()
        tx = {"from": sender, "to": contract.address, "data": tx["data"]}
        # https://geth.ethereum.org/docs/developers/evm-tracing/built-in-tracers#js-tracers
        tracers = [
            "bigramTracer",
            "evmdisTracer",
            "opcountTracer",
            "trigramTracer",
            "unigramTracer",
            """{
                data: [],
                fault: function(log) {},
                step: function(log) {
                    if(log.op.toString() == "POP") this.data.push(log.stack.peek(0));
                },
                result: function() { return this.data; }
            }""",
            """{
                retVal: [],
                step: function(log,db) {
                    this.retVal.push(log.getPC() + ":" + log.op.toString())
                },
                fault: function(log,db) {
                    this.retVal.push("FAULT: " + JSON.stringify(log))
                },
                result: function(ctx,db) {
                    return this.retVal
                }
            }
            """,
        ]
        res = []
        call = w3.provider.make_request
        with ThreadPoolExecutor(len(tracers)) as exec:
            params = [[tx, "latest", {"tracer": tracer}] for tracer in tracers]
            exec_map = exec.map(call, itertools.repeat(method), params)
            res = [json.dumps(resp["result"], sort_keys=True) for resp in exec_map]
        return res

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == EXPECTED_JS_TRACERS, res


def test_tracecall_struct_tracer(ethermint, geth):
    method = "debug_traceCall"
    acc = derive_random_account()
    sender = acc.address
    receiver = ADDRS["signer2"]

    def process(w3, gas):
        fund_acc(w3, acc)
        tx = {"from": sender, "to": receiver, "value": hex(100)}
        if gas is not None:
            # set gas limit in tx
            tx["gas"] = hex(gas)
        tx_res = w3.provider.make_request(method, [tx, "latest"])
        assert "result" in tx_res
        return tx_res["result"]

    providers = [ethermint.w3, geth.w3]
    gas = 21000
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3, gas) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == EXPECTED_STRUCT_TRACER, res

    # no gas limit set in tx
    res = process(ethermint.w3, None)
    assert res == EXPECTED_STRUCT_TRACER | {
        "gas": EXPECTED_DEFAULT_GASCAP / 2,
    }, res


def test_tracecall_prestate_tracer(ethermint, geth):
    method = "debug_traceCall"
    tracer = {"tracer": "prestateTracer"}
    sender_acc = derive_random_account()
    sender = sender_acc.address
    receiver_acc = derive_random_account()
    receiver = receiver_acc.address
    addrs = [sender.lower(), receiver.lower()]

    def process(w3):
        fund_acc(w3, sender_acc)
        fund_acc(w3, receiver_acc)
        tx = {"value": 1, "gas": 21000, "gasPrice": 88500000000}
        # make a transaction make sure the nonce is not 0
        send_transaction(w3, tx | {"from": sender, "to": receiver}, key=sender_acc.key)
        tx = tx | {"from": receiver, "to": sender}
        send_transaction(w3, tx, key=receiver_acc.key)
        tx = {"from": sender, "to": receiver, "value": hex(1)}
        tx_res = w3.provider.make_request(method, [tx, "latest", tracer])
        assert "result" in tx_res
        assert all(
            tx_res["result"][addr.lower()]
            == {
                "balance": hex(w3.eth.get_balance(addr)),
                "nonce": w3.eth.get_transaction_count(addr),
            }
            for addr in [sender, receiver]
        ), tx_res["result"]
        return tx_res["result"]

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert all(res[0][addr] == res[-1][addr] for addr in addrs), res


def test_tracecall_diff(ethermint, geth):
    method = "debug_traceCall"
    tracer = {"tracer": "prestateTracer", "tracerConfig": {"diffMode": True}}
    sender_acc = derive_new_account(4)
    sender = sender_acc.address
    receiver = derive_new_account(5).address
    fund = 3000000000000000000
    gas = 21000
    price = 88500000000
    fee = gas * price

    def process(w3):
        fund_acc(w3, sender_acc)
        tx = {"from": sender, "to": receiver, "value": 1, "gas": gas, "gasPrice": price}
        send_transaction(w3, tx, key=sender_acc.key)
        res = send_transaction(w3, tx, key=sender_acc.key)
        send_transaction(w3, tx, key=sender_acc.key)
        tx = {"from": sender, "to": receiver, "value": hex(1)}
        tx_res = w3.provider.make_request(method, [tx, hex(res["blockNumber"]), tracer])
        return json.dumps(tx_res["result"], sort_keys=True)

    providers = [ethermint.w3, geth.w3]
    expected = json.dumps(
        {
            "post": {
                receiver.lower(): {"balance": hex(3)},
                sender.lower(): {"balance": hex(fund - 3 - fee * 2), "nonce": 3},
            },
            "pre": {
                receiver.lower(): {"balance": hex(2)},
                sender.lower(): {"balance": hex(fund - 2 - fee * 2), "nonce": 2},
            },
        },
        sort_keys=True,
    )
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == expected, res


def test_debug_tracecall_call_tracer(ethermint, geth):
    method = "debug_traceCall"
    acc = derive_random_account()
    sender = acc.address
    receiver = ADDRS["signer2"]

    def process(w3, gas):
        fund_acc(w3, acc)
        tx = {"from": sender, "to": receiver, "value": hex(1)}
        if gas is not None:
            # set gas limit in tx
            tx["gas"] = hex(gas)
        tx_res = w3.provider.make_request(
            method,
            [tx, "latest", {"tracer": "callTracer"}],
        )
        assert "result" in tx_res
        return tx_res["result"]

    providers = [ethermint.w3, geth.w3]
    gas = 21000
    expected = {
        "type": "CALL",
        "from": sender.lower(),
        "to": receiver.lower(),
        "value": hex(1),
        "gas": hex(gas),
        "gasUsed": hex(gas),
        "input": "0x",
    }
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3, gas) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == expected, res

    # no gas limit set in tx
    res = process(ethermint.w3, None)
    assert res == expected | {
        "gas": hex(EXPECTED_DEFAULT_GASCAP),
        "gasUsed": hex(int(EXPECTED_DEFAULT_GASCAP / 2)),
    }, res


def test_debug_tracecall_state_overrides(ethermint, geth):
    balance = "0xffffffff"

    def process(w3):
        # generate random address, set balance in stateOverrides,
        # use prestateTracer to check balance
        address = w3.eth.account.create().address
        tx = {
            "from": address,
            "to": ADDRS["signer2"],
            "value": hex(1),
        }
        config = {
            "tracer": "prestateTracer",
            "stateOverrides": {
                address: {
                    "balance": balance,
                },
            },
        }
        tx_res = w3.provider.make_request("debug_traceCall", [tx, "latest", config])
        assert "result" in tx_res
        tx_res = tx_res["result"]
        return tx_res[address.lower()]["balance"]

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == balance, res


def test_debug_tracecall_return_revert_data_when_call_failed(ethermint, geth):
    expected = "08c379a00000000000000000000000000000000000000000000000000000000000000020000000000000000000000000000000000000000000000000000000000000001a46756e6374696f6e20686173206265656e207265766572746564000000000000"  # noqa: E501

    def process(w3):
        test_revert, _ = deploy_contract(w3, CONTRACTS["TestRevert"])
        tx_res = w3.provider.make_request(
            "debug_traceCall",
            [
                {
                    "value": "0x0",
                    "to": test_revert.address,
                    "from": ADDRS["validator"],
                    "data": "0x9ffb86a5",
                },
                "latest",
            ],
        )
        assert "result" in tx_res
        tx_res = tx_res["result"]
        return tx_res["returnValue"]

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == expected, res


def test_debug_tracecall_block_overrides(ethermint, geth):
    method = "debug_traceCall"
    gas = hex(65535)
    price = hex(88500000000)
    # https://github.com/ethereum/go-ethereum/blob/v1.11.6/core/vm/opcodes.go#L95
    tx = {"from": ADDRS["validator"], "input": "0x43", "gas": gas, "gasPrice": price}
    future_blk = "0x1337"
    tracer = {
        "blockOverrides": {
            "number": future_blk,
            "coinbase": "0x1111111111111111111111111111111111111111",
            "difficulty": hex(2),
            "time": hex(3),
            "baseLimit": hex(4),
            "baseFee": hex(5),
        }
    }

    def process(w3):
        w3_wait_for_new_blocks(w3, 1)
        tx_res = w3.provider.make_request(method, [tx, "latest", tracer])
        return json.dumps(tx_res["result"], sort_keys=True)

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1] == EXPECTED_BLOCK_OVERRIDES_TRACERS, res


def test_trace_staticcall(ethermint, geth):
    method = "debug_traceTransaction"
    tracer = {"tracer": "callTracer"}
    acc = derive_new_account(6)
    acc1 = derive_new_account(7)
    price = 58500000000
    func = "callCalculator()"
    selector = f"0x{Web3.keccak(text=func).hex()[2:10]}"
    x = "0x0000000000000000000000000000000000000000000000000000000000000000"
    y = "0x0000000000000000000000000000000000000000000000000000000000000001"

    def process(w3):
        fund_acc(w3, acc)
        fund_acc(w3, acc1)
        calculator, _ = deploy_contract(w3, CONTRACTS["Calculator"], key=acc.key)
        caller, _ = deploy_contract(
            w3,
            CONTRACTS["Caller"],
            (calculator.address,),
            key=acc.key,
        )
        w3_wait_for_new_blocks(w3, 1, sleep=0.1)
        tx = {"to": caller.address, "data": selector, "gasPrice": price}
        txs = {key: tx for key in [acc.key, acc1.key]}
        txs[acc1.key] = txs[acc1.key] | {
            "accessList": [
                {
                    "address": calculator.address,
                    "storageKeys": (x, y),
                }
            ]
        }
        sended_hash_set = send_txs(w3, txs)
        for txhash in sended_hash_set:
            res = w3.eth.wait_for_transaction_receipt(txhash, timeout=10)
        res = []
        call = w3.provider.make_request
        with ThreadPoolExecutor(len(sended_hash_set)) as exec:
            params = [[tx_hash.hex(), tracer] for tx_hash in sended_hash_set]
            exec_map = exec.map(call, itertools.repeat(method), params)
            res = [json.dumps(resp["result"], sort_keys=True) for resp in exec_map]
        return res

    providers = [ethermint.w3, geth.w3]
    with ThreadPoolExecutor(len(providers)) as exec:
        tasks = [exec.submit(process, w3) for w3 in providers]
        res = [future.result() for future in as_completed(tasks)]
        assert len(res) == len(providers)
        assert res[0] == res[-1], res
