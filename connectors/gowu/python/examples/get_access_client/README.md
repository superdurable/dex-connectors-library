# gowu Query example

`flow.py` defines a runnable Dex Flow with the static `gowu` connection. `worker.py` starts the Worker and `integration_test.py` verifies a provider 503 retry through a real Dex Worker and Client.

Start `dexcli dev -open=false`, set `DEX_FLOW_SERVICE_ADDRESS`, then run `uv run pytest examples/query/integration_test.py`. The provider is an inert HTTP fake; live provider authorization remains unverified.
