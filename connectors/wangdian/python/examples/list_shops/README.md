# Wangdian listShops example

Run `uv run python -m examples.list_shops.worker` after configuring the static `wangdian` connection in Dex Web. Run `uv run pytest examples/list_shops/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
