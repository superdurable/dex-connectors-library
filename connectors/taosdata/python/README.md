# TDengine Cloud Python connector

`dex-connector-taosdata` exposes the `listDatabases` Query Step at `dex_connectors.taosdata`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `taosdata` connection using the [connector guide](../README.md).

The [runnable listDatabases Flow](examples/list_databases/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/taosdata-python-fdg --python .venv/bin/python examples/list_databases/flow.py
```
