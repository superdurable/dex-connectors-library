# Mingdao Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `mingdao` connection in Dex Web and start `uv run --project connectors/mingdao/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `ListWorksheetsInput` and the static `mingdao` connection. Its real Worker and Client integration test is `uv run --project connectors/mingdao/python pytest connectors/mingdao/python/examples/query/integration_test.py`.
