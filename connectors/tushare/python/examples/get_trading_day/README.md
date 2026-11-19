# Tushare getTradingDay example

Run `uv run python -m examples.get_trading_day.worker` after configuring the static `tushare` connection in Dex Web. Run `uv run pytest examples/get_trading_day/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
