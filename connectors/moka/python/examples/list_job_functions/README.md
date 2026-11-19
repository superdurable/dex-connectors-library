# Moka listJobFunctions example

Run `uv run python -m examples.list_job_functions.worker` after configuring the static `moka` connection in Dex Web. Run `uv run pytest examples/list_job_functions/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
