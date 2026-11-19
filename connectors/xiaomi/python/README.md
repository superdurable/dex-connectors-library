# Xiaomi Python connector

`dex-connector-xiaomi` exposes the `listModels` Query Step through `dex_connectors.xiaomi`. Configure the static `xiaomi` connection using the [connector guide](../README.md).

The [runnable listModels Flow](examples/list_models/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/xiaomi-python-fdg --python .venv/bin/python examples/list_models/flow.py
```
