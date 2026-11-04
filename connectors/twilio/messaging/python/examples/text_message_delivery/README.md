# Twilio message delivery Flow

This runnable Flow sends one message through the static `twilio-messaging` connection and reads its status. The example wires the happy paths `accepted` and `found`. Optional rejection, uncertain, invalid response, and defect branches fail the Flow if selected. A send response can be accepted while delivery is still queued; the returned status is a snapshot.

Generate the Go example's strict FDG 2.0 reference from `connectors/twilio/messaging`:

```sh
mkdir -p build
dexcli visualize ./go/examples/text-message-delivery/flow/workflow.go --schema-version 2.0 --json --out ./build/text-message-delivery
```

Start `dexcli dev -flow-rendering-dir ./build/text-message-delivery`, then configure the `twilio-messaging` connection in Dex Web Connections. Select either Account SID/Auth Token or API key authentication and supply a sender or set `sender` in the input. The Python Worker reads the resulting owner-only connection file; credentials never enter Flow state.

From this package directory:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.text_message_delivery.main
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json uv run python -m examples.text_message_delivery.start twilio-message-001 examples/text_message_delivery/sample-input.json
```

The checked-in `integration_test.py` runs this same Flow through a real Dex Worker and Client with an inert provider stand-in. Live Twilio authorization and delivery remain unverified until the author tests the connection through Dex Web.
