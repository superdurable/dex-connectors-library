# Yanwen Python connector

`dex-connector-yanwen` exposes the `list_warehouses` Query Step. Configure the static `yanwen` connection with the [provider guide](../README.md).

The [runnable Flow](examples/list_warehouses/) includes a Worker entrypoint and real Dex retry integration test with synthetic provider responses. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yanwen-python-fdg --python .venv/bin/python examples/list_warehouses/flow.py
```
