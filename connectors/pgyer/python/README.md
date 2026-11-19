# pgyer Python connector

The `dex-connector-pgyer` package implements the `listUploadedApps` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/uploaded_apps/flow.py) uses the static `pgyer` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.

## Local verification

```bash
uv run --locked pytest tests/test_conformance.py
uv run --locked ruff check src tests examples
uv run --locked mypy src examples
dexcli visualize ./examples/uploaded_apps/flow.py --schema-version 2.0 --json --open=false --out /tmp/pgyer-python-fdg
```

Use the [runnable example](examples/uploaded_apps/README.md) for Dex Web setup and the Worker entrypoint.
