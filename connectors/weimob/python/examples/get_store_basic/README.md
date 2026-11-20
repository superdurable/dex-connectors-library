# Weimob getStoreBasic example

Run `uv run python -m examples.get_store_basic.worker` after configuring the static `weimob` connection in Dex Web. Run `uv run pytest examples/get_store_basic/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
