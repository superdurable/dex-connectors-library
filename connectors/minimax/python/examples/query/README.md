# Minimax Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `minimax` connection in Dex Web and start `uv run --project connectors/minimax/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `GetTokenPlanRemainsInput` and the static `minimax` connection. Its real Worker and Client integration test is `uv run --project connectors/minimax/python pytest connectors/minimax/python/examples/query/integration_test.py`.
