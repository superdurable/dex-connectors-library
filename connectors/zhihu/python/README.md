# Zhihu Python connector

The `dex-connector-zhihu` package implements the `listQuotas` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/quota_list/flow.py) uses the static `zhihu` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.
