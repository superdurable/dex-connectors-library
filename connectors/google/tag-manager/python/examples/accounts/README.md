# Google Tag Manager accounts Flow (Python)

This runnable Dex Flow lists accessible Tag Manager accounts and persists their paths and display names. Configure the static `google-tag-manager` OAuth connection using the [connector guide](../../../README.md).

From this package, run `uv sync --group dev`. Generate its FDG with `dexcli visualize examples/accounts/flow.py --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/tagmanager-python-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/accounts/integration_test.py` to check a 503 retry through a real Dex Worker and Client with a synthetic token. For a real account, run `uv run python -m examples.accounts.worker`; live Google authorization remains unverified.
