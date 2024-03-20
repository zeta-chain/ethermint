from concurrent.futures import ThreadPoolExecutor, as_completed

import pytest
from web3 import Web3

from .network import setup_custom_ethermint
from .utils import (
    ADDRS,
    approve_proposal,
    eth_to_bech32,
    send_transaction,
    w3_wait_for_block,
    w3_wait_for_new_blocks,
)

NEW_BASE_FEE = 100000000000


@pytest.fixture(scope="module")
def custom_ethermint(tmp_path_factory):
    path = tmp_path_factory.mktemp("fee-history")
    yield from setup_ethermint(path, 26500, long_timeout_commit=True)


@pytest.fixture(scope="module", params=["ethermint", "geth"])
def cluster(request, custom_ethermint, geth):
    """
    run on both ethermint and geth
    """
    provider = request.param
    if provider == "ethermint":
        yield custom_ethermint
    elif provider == "geth":
        yield geth
    else:
        raise NotImplementedError


def test_basic(cluster):
    w3: Web3 = cluster.w3
    # need at least 5 blocks
    w3_wait_for_block(w3, 5)
    call = w3.provider.make_request
    tx = {"to": ADDRS["community"], "value": 10, "gasPrice": w3.eth.gas_price}
    send_transaction(w3, tx)
    size = 4
    # size of base fee + next fee
    max = size + 1
    # only 1 base fee + next fee
    min = 2
    method = "eth_feeHistory"
    field = "baseFeePerGas"
    percentiles = [100]
    height = w3.eth.block_number
    latest = dict(
        blocks=["latest", hex(height)],
        expect=max,
    )
    earliest = dict(
        blocks=["earliest", "0x0"],
        expect=min,
    )
    for tc in [latest, earliest]:
        res = []
        with ThreadPoolExecutor(len(tc["blocks"])) as exec:
            tasks = [
                exec.submit(call, method, [size, b, percentiles]) for b in tc["blocks"]
            ]
            res = [future.result()["result"][field] for future in as_completed(tasks)]
        assert len(res) == len(tc["blocks"])
        assert res[0] == res[1]
        assert len(res[0]) == tc["expect"]

    for x in range(max):
        i = x + 1
        fee_history = call(method, [size, hex(i), percentiles])
        # start to reduce diff on i <= size - min
        diff = size - min - i
        reduce = size - diff
        target = reduce if diff >= 0 else max
        res = fee_history["result"]
        assert len(res[field]) == target
        oldest = i + min - max
        assert res["oldestBlock"] == hex(oldest if oldest > 0 else 0)


def test_change(cluster):
    w3: Web3 = cluster.w3
    call = w3.provider.make_request
    tx = {"to": ADDRS["community"], "value": 10, "gasPrice": w3.eth.gas_price}
    send_transaction(w3, tx)
    size = 4
    method = "eth_feeHistory"
    field = "baseFeePerGas"
    percentiles = [100]
    for b in ["latest", hex(w3.eth.block_number)]:
        history0 = call(method, [size, b, percentiles])["result"][field]
        w3_wait_for_new_blocks(w3, 2, 0.1)
        history1 = call(method, [size, b, percentiles])["result"][field]
        if b == "latest":
            assert history1 != history0
        else:
            assert history1 == history0


def adjust_base_fee(parent_fee, gas_limit, gas_used, params):
    "spec: https://eips.ethereum.org/EIPS/eip-1559#specification"
    change_denominator = params["base_fee_change_denominator"]
    elasticity_multiplier = params["elasticity_multiplier"]
    gas_target = gas_limit // elasticity_multiplier
    if gas_used == gas_target:
        return parent_fee
    delta = parent_fee * abs(gas_target - gas_used) // gas_target // change_denominator
    # https://github.com/crypto-org-chain/ethermint/blob/develop/x/feemarket/keeper/eip1559.go#L104
    if gas_target > gas_used:
        return max(parent_fee - delta, int(float(params["min_gas_price"])))
    else:
        return parent_fee + max(delta, 1)


def test_next(cluster, custom_ethermint):
    def params_fn(height):
        if cluster == custom_ethermint:
            return cluster.cosmos_cli().get_params("feemarket", height=height)["params"]
        return {
            "elasticity_multiplier": 2,
            "base_fee_change_denominator": 8,
            "min_gas_price": 0,
        }

    w3: Web3 = cluster.w3
    call = w3.provider.make_request
    tx = {"to": ADDRS["community"], "value": 10, "gasPrice": w3.eth.gas_price}
    send_transaction(w3, tx)
    assert_histories(w3, call, w3.eth.block_number, params_fn, percentiles=[100])


def test_beyond_head(cluster):
    end = hex(0x7FFFFFFFFFFFFFFF)
    res = cluster.w3.provider.make_request("eth_feeHistory", [4, end, []])
    msg = f"request beyond head block: requested {int(end, 16)}"
    assert msg in res["error"]["message"]


def test_percentiles(cluster):
    w3: Web3 = cluster.w3
    call = w3.provider.make_request
    method = "eth_feeHistory"
    percentiles = [[-1], [101], [2, 1]]
    size = 4
    msg = "invalid reward percentile"
    with ThreadPoolExecutor(len(percentiles)) as exec:
        tasks = [exec.submit(call, method, [size, "latest", p]) for p in percentiles]
        result = [future.result() for future in as_completed(tasks)]
        assert all(msg in res["error"]["message"] for res in result)


def update_feemarket_param(node, tmp_path, new_multiplier=2, new_denominator=200000000):
    cli = node.cosmos_cli()
    p = cli.get_params("feemarket")["params"]
    new_base_fee = f"{NEW_BASE_FEE}"
    p["base_fee"] = new_base_fee
    p["elasticity_multiplier"] = new_multiplier
    p["base_fee_change_denominator"] = new_denominator
    proposal = tmp_path / "proposal.json"
    # governance module account as signer
    data = hashlib.sha256("gov".encode()).digest()[:20]
    signer = eth_to_bech32(data)
    proposal_src = {
        "messages": [
            {
                "@type": "/ethermint.feemarket.v1.MsgUpdateParams",
                "authority": signer,
                "params": p,
            }
        ],
        "deposit": "2aphoton",
        "title": "title",
        "summary": "summary",
    }
    proposal.write_text(json.dumps(proposal_src))
    rsp = cli.submit_gov_proposal(proposal, from_="community")
    assert rsp["code"] == 0, rsp["raw_log"]
    approve_proposal(node, rsp, status=3)
    print("check params have been updated now")
    p = cli.get_params("feemarket")["params"]
    assert p["base_fee"] == new_base_fee
    assert p["elasticity_multiplier"] == new_multiplier
    assert p["base_fee_change_denominator"] == new_denominator


def test_concurrent(custom_ethermint, tmp_path):
    w3: Web3 = custom_ethermint.w3
    tx = {"to": ADDRS["community"], "value": 10, "gasPrice": w3.eth.gas_price}
    # send multi txs, overlap happens with query with 2nd tx's block number
    send_transaction(w3, tx)
    receipt1 = send_transaction(w3, tx)
    b1 = receipt1.blockNumber
    send_transaction(w3, tx)
    call = w3.provider.make_request
    field = "baseFeePerGas"
    update_feemarket_param(custom_ethermint, tmp_path)
    percentiles = []
    method = "eth_feeHistory"
    # big enough concurrent requests to trigger overwrite bug
    total = 10
    size = 2
    params = [size, hex(b1), percentiles]
    res = []
    with ThreadPoolExecutor(total) as exec:
        t = [exec.submit(call, method, params) for i in range(total)]
        res = [future.result()["result"][field] for future in as_completed(t)]
    assert all(sublist == res[0] for sublist in res), res


def assert_histories(w3, call, blk, params_fn, percentiles=[]):
    method = "eth_feeHistory"
    field = "baseFeePerGas"
    expected = []
    blocks = []
    histories = []
    for i in range(3):
        b = blk + i
        blocks.append(b)
        history = tuple(call(method, [1, hex(b), percentiles])["result"][field])
        histories.append(history)
        w3_wait_for_new_blocks(w3, 1, 0.1)
    blocks.append(b + 1)

    for b in blocks:
        next_base_price = w3.eth.get_block(b).baseFeePerGas
        prev = b - 1
        blk = w3.eth.get_block(prev)
        base_fee = blk.baseFeePerGas
        params = params_fn(prev)
        res = adjust_base_fee(
            base_fee,
            blk.gasLimit,
            blk.gasUsed,
            params,
        )
        if abs(next_base_price - res) == 1:
            next_base_price = res
        elif next_base_price != NEW_BASE_FEE:
            assert next_base_price == res
        expected.append(hex(next_base_price))
    assert histories == list(zip(expected, expected[1:]))


def test_param_change(custom_ethermint, tmp_path):
    def params_fn(height):
        cli = custom_ethermint.cosmos_cli()
        return cli.get_params("feemarket", height=height)["params"]

    w3: Web3 = custom_ethermint.w3
    old_blk = w3.eth.block_number
    update_feemarket_param(custom_ethermint, tmp_path)
    call = w3.provider.make_request
    assert_histories(w3, call, old_blk, params_fn)
    tx = {"to": ADDRS["community"], "value": 10, "gasPrice": w3.eth.gas_price}
    receipt = send_transaction(w3, tx)
    new_blk = receipt.blockNumber
    assert_histories(w3, call, new_blk, params_fn)
