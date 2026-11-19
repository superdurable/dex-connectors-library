# Zadig Python connector

The `dex-connector-zadig` package implements the `listProjects` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/project_list/flow.py) uses the static `zadig` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.
