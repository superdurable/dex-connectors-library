# Maiyun Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `maiyun` connection in Dex Web and start `uv run --project connectors/maiyun/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `LookupIPLocationInput` and the static `maiyun` connection. Its real Worker and Client integration test is `uv run --project connectors/maiyun/python pytest connectors/maiyun/python/examples/query/integration_test.py`.
