# Meiqia Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `meiqia` connection in Dex Web and start `uv run --project connectors/meiqia/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `GetConversationInput` and the static `meiqia` connection. Its real Worker and Client integration test is `uv run --project connectors/meiqia/python pytest connectors/meiqia/python/examples/query/integration_test.py`.
