# Navinfo lookupIPLocation example

Run `uv run python -m examples.lookup_ip_location.worker` after configuring the static `navinfo` connection in Dex Web. Run `uv run pytest examples/lookup_ip_location/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
