# opencsg Python connector

The `dex-connector-opencsg` package implements the `listGatewayModels` Query. It replays Go-recorded provider scenarios in [tests/test_conformance.py](tests/test_conformance.py).

The [runnable Flow](examples/gateway_models/flow.py) uses the static `opencsg` connection. Its integration test exercises retry through a real Dex Worker and Client. Live provider authorization remains unverified.

## Local verification

```bash
uv run --locked pytest tests/test_conformance.py
uv run --locked ruff check src tests examples
uv run --locked mypy src examples
dexcli visualize ./examples/gateway_models/flow.py --schema-version 2.0 --json --open=false --out /tmp/opencsg-python-fdg
```

Use the [runnable example](examples/gateway_models/README.md) for Dex Web setup and the Worker entrypoint.
