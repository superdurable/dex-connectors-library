# Google Analytics 4 property Flow (Python)

This runnable Dex Flow reads one Analytics 4 property and persists its name, time zone, and currency. Configure the static `google-analytics-4` OAuth connection using the [connector guide](../../../README.md).

From this package, run `uv sync --group dev`. Generate its FDG with `dexcli visualize examples/property/flow.py --python .venv/bin/python --schema-version 2.0 --lint app --json --out /tmp/analytics4-python-fdg --open=false`. With `dexcli dev` running, set `DEX_FLOW_SERVICE_ADDRESS` and run `uv run pytest examples/property/integration_test.py` to check a 503 retry through a real Dex Worker and Client with a synthetic token. For a real account, run `uv run python -m examples.property.worker`; live Google authorization remains unverified.
