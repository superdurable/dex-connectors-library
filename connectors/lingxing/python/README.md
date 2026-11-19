# lingxing Python connector

The `listStores` Query reads the provider through a Dex Step and returns only the generated safe output. See `examples/query/` for a runnable Worker, Flow, and real Dex integration test.

Configure the credential in Dex Web Connections using the authorization guide in `../connector.yaml`. The static connection name is `lingxing`. The provider call runs only in the Query Step.

Run `uv run pytest tests/test_conformance.py` to replay the Go-recorded requests and outcomes. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:7233 uv run pytest examples/query/integration_test.py` against `dexcli dev -open=false` for retry and persistence verification. Real provider authorization has not been exercised.
