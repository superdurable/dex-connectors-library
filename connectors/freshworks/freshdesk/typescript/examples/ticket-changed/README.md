# Freshdesk ticket changes

`TicketChangedFlow` (`FreshdeskTicketChange`) starts once per Freshdesk ticket created or updated, through the `ticketChanged` poll Trigger. It records the delivered change, reads the ticket back with `getTicket` for its description, requester, and first conversations, and completes with a `TicketChangeOutcome`.

Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create a Freshdesk connection named `freshdesk-ticket-watcher` with the helpdesk `domain` and the agent's `api_key`, as the [connector README](../../../README.md#freshdesk-setup) describes; `pollInterval` is optional. Optionally save the `helpdesk-ticket-changes` binding's configuration, for example `{"changeKinds": ["created"]}`; unsaved, it delivers every change. Freshdesk setup commands cannot send the HTTP Basic API key, so the binding has no picker units. Point `DEX_CONNECTOR_CONFIG_FILE` at the local mode-0600 `connections.json` written by Dex Web. From `connectors/freshworks/freshdesk/typescript`, run:

```sh
npm run example:ticket-changed
```

The first poll records a baseline and delivers no existing ticket. A Dex Web project keeps the poll cursor in process memory, so a local restart records a new baseline; a hosted project keeps it in project storage. `FRESHDESK_LOCAL_API_BASE_URL` points the Worker at a local Freshdesk-compatible fake for verification only. `LOG_LEVEL=debug` shows every delivery.

`flow/workflow.ts` declares the binding, `app.ts` routes it into the Flow through the binding's durable inbox and runs the Worker, and `main.ts` is the entrypoint. Run the real Dex integration tests against the stateful ticket-list fake:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21040 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0:

```sh
npm run build
npm install --prefix examples/ticket-changed --no-save --no-package-lock --ignore-scripts .
cd examples/ticket-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Freshdesk polling remains unverified.
