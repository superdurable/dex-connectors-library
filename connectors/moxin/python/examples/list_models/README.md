# Moxin listModels example

Run `uv run python -m examples.list_models.worker` after configuring the static `moxin` connection in Dex Web. Run `uv run pytest examples/list_models/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
