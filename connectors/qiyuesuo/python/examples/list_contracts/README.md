# Qiyuesuo listContracts example

Run `uv run python -m examples.list_contracts.worker` after configuring the static `qiyuesuo` connection in Dex Web. Run `uv run pytest examples/list_contracts/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
