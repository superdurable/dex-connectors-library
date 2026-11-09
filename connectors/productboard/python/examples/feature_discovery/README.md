# Productboard Python feature discovery

This runnable Flow calls `listFeatures` through the static `productboard` connection, persists the selected page, and completes. Optional branches are unwired and fail the Flow when selected. Configure `api_token` in Dex Web Connections as described in the [connector README](../../README.md).

From `connectors/productboard/python`, run `uv run python -m examples.feature_discovery.worker` with the project's `DEX_PROJECT_*` environment and a running `dexcli dev` service. Start Flow type `ProductboardFeatureDiscoveryPython` with a unique Flow ID and input `{"pageCursor":""}`. Copy the returned `nextCursor` into `pageCursor` to read another page.

Run `uv run pytest -q` for deterministic package tests. With `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex service, run `uv run pytest examples/feature_discovery/integration_test.py -q` for the real Worker and Client path.
