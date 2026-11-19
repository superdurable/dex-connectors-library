# ZTO queryWaybillTrace example

Run `uv run python -m examples.query_waybill_trace.worker` after configuring the static `zto` connection in Dex Web. Run `uv run pytest examples/query_waybill_trace/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
