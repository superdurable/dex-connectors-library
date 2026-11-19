# TianAPI Python connector

`dex-connector-tianapi` exposes the `getHolidayStatus` Query Step at `dex_connectors.tianapi`. It filters provider data to the Go-defined output model and never persists credentials or raw responses. Configure the static `tianapi` connection using the [connector guide](../README.md).

The [runnable getHolidayStatus Flow](examples/get_holiday_status/) includes a real Dex Worker and Client integration test with a fake provider. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/tianapi-python-fdg --python .venv/bin/python examples/get_holiday_status/flow.py
```
