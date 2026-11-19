# Yunpian Python connector

The `dex-connector-yunpian` package implements the `getAccountBalance` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/account_balance/flow.py) uses the static `yunpian` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.
