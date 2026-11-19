# ZoomEye getAccountQuota example

Run `uv run python -m examples.account_quota.worker` after configuring the static `zoomeye` connection in Dex Web. Run `uv run pytest examples/account_quota/integration_test.py` against a local Dex stack. The integration test uses fake provider responses; live provider behavior remains unverified.
