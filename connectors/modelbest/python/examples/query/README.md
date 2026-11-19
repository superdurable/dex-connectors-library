# Modelbest Python Query Flow

Run `dexcli dev -open=false` in an isolated directory, then configure the `modelbest` connection in Dex Web and start `uv run --project connectors/modelbest/python python -m examples.query.worker` with `DEX_FLOW_SERVICE_ADDRESS` set to the local Dex address. The Flow uses `ListModelsInput` and the static `modelbest` connection. Its real Worker and Client integration test is `uv run --project connectors/modelbest/python pytest connectors/modelbest/python/examples/query/integration_test.py`.
