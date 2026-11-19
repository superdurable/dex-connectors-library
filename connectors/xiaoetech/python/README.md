# XiaoeTech Python connector

`dex-connector-xiaoetech` exposes the `list_root_goods_categories` Query Step. Configure the static `xiaoetech` connection with the [provider guide](../README.md).

The [runnable Flow](examples/list_root_goods_categories/) includes a Worker entrypoint and real Dex retry integration test with synthetic provider responses. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/xiaoetech-python-fdg --python .venv/bin/python examples/list_root_goods_categories/flow.py
```
