import asyncio
import json
from collections import defaultdict

import websockets
from pystarport import ports


def test_single_request_netversion(ethermint):
    ethermint.use_websocket()
    eth_ws = ethermint.w3.provider

    response = eth_ws.make_request("net_version", [])

    # net_version should be 9000
    assert response["result"] == "9000", "got " + response["result"] + ", expected 9000"

