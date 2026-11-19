# Yunda getWaybillBalance example

Run `uv run python -m examples.get_waybill_balance.worker` after configuring the static `yunda` connection in Dex Web. Run `uv run pytest examples/get_waybill_balance/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
