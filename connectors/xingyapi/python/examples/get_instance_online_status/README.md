# Xingyapi getInstanceOnlineStatus example

Run `uv run python -m examples.get_instance_online_status.worker` after configuring the static `xingyapi` connection in Dex Web. Run `uv run pytest examples/get_instance_online_status/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
