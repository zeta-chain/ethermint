import pytest

from .network import setup_ethermint, setup_geth


def pytest_configure(config):
    config.addinivalue_line("markers", "unmarked: fallback mark for unmarked tests")
    config.addinivalue_line("markers", "upgrade: upgrade tests")
    config.addinivalue_line("markers", "filter: filter tests")


def pytest_collection_modifyitems(items, config):
    for item in items:
        if not any(item.iter_markers()):
            item.add_marker("unmarked")


@pytest.fixture(scope="session")
def ethermint(tmp_path_factory):
    path = tmp_path_factory.mktemp("ethermint")
    yield from setup_ethermint(path, 26650)


@pytest.fixture(scope="session")
def geth(tmp_path_factory):
    path = tmp_path_factory.mktemp("geth")
    yield from setup_geth(path, 8545)


@pytest.fixture(scope="session", params=["ethermint", "ethermint-ws"])
def ethermint_rpc_ws(request, ethermint):
    """
    run on both ethermint and ethermint websocket
    """
    provider = request.param
    if provider == "ethermint":
        yield ethermint
    elif provider == "ethermint-ws":
        ethermint_ws = ethermint.copy()
        ethermint_ws.use_websocket()
        yield ethermint_ws
    else:
        raise NotImplementedError
