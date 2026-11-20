# Winit list_warehouses example

Run `uv run python -m examples.list_warehouses.worker` after configuring the static `winit` connection in Dex Web. Run `uv run pytest examples/list_warehouses/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
