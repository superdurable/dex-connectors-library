# Weiling listDepartments example

Run `uv run python -m examples.list_departments.worker` after configuring the static `weiling` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/list_departments/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/list_departments/integration_test.py`. The integration test uses synthetic provider responses; live Weiling authorization remains unverified.
