# West list_domains example

Run `uv run python -m examples.list_domains.worker` after configuring the static `west` connection in Dex Web. Run `uv run pytest examples/list_domains/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
