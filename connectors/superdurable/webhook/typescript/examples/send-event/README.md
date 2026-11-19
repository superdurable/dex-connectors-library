# Send a signed webhook event

`SendEventFlow` sends one JSON event with the static `webhook-events` connection. The provider request runs inside the connector Step. The Flow returns only the safe `webhookId` and receiver status.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. Configure the private Dex Web connection named `webhook-events` with the receiver's HTTPS `deliveryUrl` and shared `signing_secret`. Set `DEX_CONNECTOR_CONFIG_FILE` to the mode-0600 local Dex Web `connections.json` file. From `connectors/superdurable/webhook/typescript`, start the Worker:

```sh
npm run example:send
```

In another terminal with the same configuration, edit `input.example.json` and start the Flow:

```sh
npm run example:send:start -- examples/send-event/input.example.json
```

The receiver must deduplicate by `webhook-id` for at least the effective retry window and answer 2xx for an event it already applied. The Step wires only `delivered`; rejection and configuration defects fail the Flow. No signing secret belongs in Flow input.

Run the real Dex integration test against a local receiver stand-in:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:28901 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From this package, make the local package visible to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/send-event --no-save --no-package-lock --ignore-scripts .
cd examples/send-event
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live receiver behavior has not been verified.
