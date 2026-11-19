# Yunda Express Python connector

`dex-connector-yunda` exposes the `getWaybillBalance` Query Step through `dex_connectors.yunda`. Configure the static `yunda` connection using the [connector guide](../README.md).

The [runnable getWaybillBalance Flow](examples/get_waybill_balance/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/yunda-python-fdg --python .venv/bin/python examples/get_waybill_balance/flow.py
```
