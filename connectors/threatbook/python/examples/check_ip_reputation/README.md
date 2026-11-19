# ThreatBook checkIPReputation example

Run `uv run python -m examples.check_ip_reputation.worker` after configuring the static `threatbook` connection in Dex Web. Run `uv run pytest examples/check_ip_reputation/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
