# JisuAPI Python connector

`dex-connector-jisuapi` exposes the `listProvinces` Query Step at `dex_connectors.jisuapi`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `jisuapi` connection using the [connector guide](../README.md).

The [runnable listProvinces Flow](examples/provinces/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/jisuapi-python-fdg --python .venv/bin/python examples/provinces/flow.py
```
