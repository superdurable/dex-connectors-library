# Wapi listVehicleBrands example

Run `uv run python -m examples.list_vehicle_brands.worker` after configuring the static `wapi` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/list_vehicle_brands/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/list_vehicle_brands/integration_test.py`. The integration test uses synthetic provider responses; live Wapi authorization remains unverified.
