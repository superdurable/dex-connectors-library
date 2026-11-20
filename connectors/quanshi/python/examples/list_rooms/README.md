# Quanshi listRooms example

Run `uv run python -m examples.list_rooms.worker` after configuring the static `quanshi` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/list_rooms/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/list_rooms/integration_test.py`. The integration test uses synthetic provider responses; live Quanshi authorization remains unverified.
