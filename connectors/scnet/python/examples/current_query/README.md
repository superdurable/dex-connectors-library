# SCNet Query Flow

Run `python -m examples.current_query.worker` with a `scnet` connection configured through Dex Web. The Flow uses the operation-specific factory, preserves its result in an Attribute, and wires only the happy-path branch.

`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest -q examples/current_query/integration_test.py` exercises one 503 retry with a real `dexcli dev -open=false` server and a local fake provider. Live SCNet authorization is unverified.
