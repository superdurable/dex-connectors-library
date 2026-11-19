# Wangdian Python connector

`dex-connector-wangdian` exposes the `listShops` Query Step through `dex_connectors.wangdian`. Configure the static `wangdian` connection using the [connector guide](../README.md).

The [runnable listShops Flow](examples/list_shops/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/wangdian-python-fdg --python .venv/bin/python examples/list_shops/flow.py
```
