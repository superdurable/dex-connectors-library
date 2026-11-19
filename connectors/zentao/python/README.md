# ZenTao Python connector

The `dex-connector-zentao` package implements the `listProducts` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/product_list/flow.py) uses the static `zentao` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.
