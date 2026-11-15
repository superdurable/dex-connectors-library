# Gorgias ticket changes

`TicketChangedFlow` (`GorgiasTicketChangeTypeScript`) starts once per Gorgias ticket event, such as `ticket-created` or `ticket-closed`, through the `ticketChanged` poll Trigger. It records the delivered event, reads the ticket back with `getTicket` for its status, tags, customer, and latest messages, and completes with a `TicketChangeOutcome`. Each Flow ID is `gorgias-ticket-change-gorgias-event-<event id>`.

Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create a Gorgias connection named `gorgias-ticket-watcher` with the helpdesk `domain`, the user `email`, and the `api_key`, as the [connector README](../../../README.md#gorgias-setup) describes; `pollInterval` is optional. Optionally save the `helpdesk-ticket-events` binding's configuration, for example `{"eventTypes": ["ticket-created"]}`; unsaved, it delivers every ticket event type. Studio setup commands cannot send the HTTP Basic API key, so the binding has no picker units. Point `DEX_CONNECTOR_CONFIG_FILE` at the local mode-0600 `connections.json` written by Dex Web. From `connectors/gorgias/typescript`, run:

```sh
npm run example:ticket-changed
```

The first poll records a baseline and delivers no earlier event. A Dex Web project keeps the poll cursor in process memory, so a local restart records a new baseline; a hosted project keeps it in project storage. `GORGIAS_LOCAL_API_BASE_URL` points the Worker at a local Gorgias-compatible fake for verification only. `LOG_LEVEL=debug` shows every delivery.

`flow/workflow.ts` declares the binding, `app.ts` routes it into the Flow through the binding's durable inbox and runs the Worker, and `main.ts` is the entrypoint. Run the real Dex integration tests against the stateful helpdesk fake; they cover the baseline, one Flow per event across a restart, a 429 with `Retry-After`, and a rejected poll retried from the same cursor:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0:

```sh
npm run build
npm install --prefix examples/ticket-changed --no-save --no-package-lock --ignore-scripts .
cd examples/ticket-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Gorgias polling remains unverified.
