# Read a conversation

This example starts one Dex Flow with a conversation ID, reads the latest parts, and saves the result. Its static Dex Web connection name is `intercom-support-inbox`.

From `connectors/intercom/python`, run `uv sync`. Start `dexcli dev -dex-port 37301 -web-port 37302 -open=false` in another terminal, then set `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37301`. Configure the `intercom-support-inbox` connection in Dex Web using the connector's authorization guide, and run `uv run python -m examples.read_conversation.main`.

To exercise the same Flow against a fake provider through a real Dex Worker and Client, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37301 uv run pytest examples/read_conversation/integration_test.py`. The test sends a rate limit response followed by a successful conversation read. Live Intercom authorization and API behavior remain unverified.
