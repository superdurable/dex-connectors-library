# Tushare Python connector

`dex-connector-tushare` exposes the `getTradingDay` Query Step through `dex_connectors.tushare`. Configure the static `tushare` connection using the [connector guide](../README.md).

The [runnable getTradingDay Flow](examples/get_trading_day/) has a Worker entrypoint and real Dex integration test. Live provider authorization remains unverified.

## Strict FDG 2.0 generation

```bash
dexcli visualize --open=false --language python --schema-version 2.0 --lint app --json --out /tmp/tushare-python-fdg --python .venv/bin/python examples/get_trading_day/flow.py
```
