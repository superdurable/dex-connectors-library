# Miyang Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `miyang` connection in Dex Web and start `uv run --project connectors/miyang/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `ListModelsInput` and the static `miyang` connection. Its real Worker and Client integration test is `uv run --project connectors/miyang/python pytest connectors/miyang/python/examples/query/integration_test.py`.
