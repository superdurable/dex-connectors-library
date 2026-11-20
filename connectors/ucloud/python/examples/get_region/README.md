# Ucloud getRegion example

Run `uv run python -m examples.get_region.worker` after configuring the static `ucloud` connection in Dex Web. Run `uv run pytest examples/get_region/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
