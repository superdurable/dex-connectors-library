# Yijingtong Python connector

`dex-connector-yijingtong` exposes the `listWarehouses` Query Step through `dex_connectors.yijingtong`. Configure the static `yijingtong` connection using the [connector guide](../README.md).

The [runnable listWarehouses Flow](examples/list_warehouses/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yijingtong-python-fdg --python .venv/bin/python examples/list_warehouses/flow.py
```
