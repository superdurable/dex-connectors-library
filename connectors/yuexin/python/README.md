# Yuexin Python connector

`dex-connector-yuexin` exposes the `listModels` Query Step through `dex_connectors.yuexin`. Configure the static `yuexin` connection using the [connector guide](../README.md).

The [runnable listModels Flow](examples/list_models/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yuexin-python-fdg --python .venv/bin/python examples/list_models/flow.py
```
