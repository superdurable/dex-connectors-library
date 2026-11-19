# Volcengine describeRegions example

Run `uv run python -m examples.describe_regions.worker` after configuring the static `volcengine` connection in Dex Web. Run `uv run pytest examples/describe_regions/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
