# Signed event delivery Flow for Python

This Flow POSTs the JSON value in [`sample-input.json`](sample-input.json) to
one HTTPS receiver and completes with its status and stable webhook ID. Its
static Dex connection is `webhook / webhook-forwarding`. The `rejected` and
`defect` branches fail the Flow. The receiver should record each `webhook-id`
with the event it applies and answer 2xx when the same ID arrives again.

From `connectors/superdurable/webhook/python`, install and generate strict FDG
2.0 with the latest stable Dex CLI:

```sh
uv sync --locked
mkdir -p build
dexcli visualize examples/send_event/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out build/send-event
```

The graph must report `valid: true`. Start `dexcli dev
--flow-rendering-dir ./build`, then open Dex Web **Connections** and configure
`webhook / webhook-forwarding`. Set `deliveryUrl` to the receiver's HTTPS URL
from its endpoint or webhook settings; the URL must contain no embedded
secret. Enter `signing_secret` as the whole `whsec_` secret issued by a
Standard Webhooks receiver or another shared secret it expects. The
[connector setup guide](../../../README.md#sender-setup) explains the other
authorization and incoming request fields. They are unnecessary for this
outgoing-only Flow. The Step has no additional configuration units.

Point `DEX_CONNECTOR_CONFIG_FILE` at the owner-only `connections.json` written
by the Dex Web project. Run the Worker and then start with a unique Flow ID:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.send_event.worker

DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.send_event.start \
  webhook-send-001 examples/send_event/sample-input.json
```

With `dexcli dev` running, the checked-in integration test uses its real
Worker and Client while a credential-safe fake receiver verifies the
signature, returns 503 once, and then acknowledges the repeated webhook ID:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/send_event/integration_test.py
```

This test does not verify a live receiver or real Dex Web authorization.
