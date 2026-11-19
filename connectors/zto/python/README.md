# ZTO Python connector

`dex-connector-zto` exposes the `queryWaybillTrace` Query Step through `dex_connectors.zto`. Configure the static `zto` connection using the [connector guide](../README.md).

The [runnable queryWaybillTrace Flow](examples/query_waybill_trace/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/zto-python-fdg --python .venv/bin/python examples/query_waybill_trace/flow.py
```
