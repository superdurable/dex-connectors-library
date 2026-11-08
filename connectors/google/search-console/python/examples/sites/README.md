# Google Search Console sites Flow (Python)

This runnable Dex Flow lists Search Console properties and persists their URLs and permission levels. Configure the static `google-search-console` OAuth connection using the [connector guide](../../../README.md).

From this package, run `uv sync --group dev`. Generate its FDG with `dexcli visualize examples/sites/flow.py --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/search-console-python-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/sites/integration_test.py` to check a 503 retry through a real Dex Worker and Client with a synthetic token. For a real account, run `uv run python -m examples.sites.worker`; live Google authorization remains unverified.
