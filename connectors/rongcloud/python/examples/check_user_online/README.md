# RongCloud checkUserOnline example

Run `uv run python -m examples.check_user_online.worker` after configuring the static `rongcloud` connection in Dex Web. Run `dexcli visualize --open=false` on `examples/check_user_online/flow.py` to inspect the Flow graph. With a local Dex stack and `DEX_FLOW_SERVICE_ADDRESS`, run `uv run pytest examples/check_user_online/integration_test.py`. The integration test uses synthetic provider responses; live RongCloud authorization remains unverified.
