# Zendesk ticket changes, TypeScript

`TicketChangedFlow` (`ZendeskTicketChangeTypeScript`) starts once per Zendesk ticket created, updated, or deleted, through the `ticketChanged` poll Trigger. It records the delivered change, completes at once for a deleted ticket, and otherwise reads the ticket back with `getTicket` and its latest comment, then completes with a `TicketChangeOutcome`.

Start a Dex development server and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web Connections, create a Zendesk Support connection named `zendesk-ticket-watcher` with the account `subdomain`, an administrator's `email`, and that administrator's `api_token`, as the [connector README](../../../README.md#zendesk-setup) describes; Zendesk allows the incremental ticket export only for administrators. `pollInterval` is optional. Optionally save the `zendesk-ticket-changes` binding's configuration as `{"includeDeleted": true}` to also receive deletions; unsaved, it delivers created and updated tickets. Zendesk has no setup bundle, so the binding has no configuration unit. Point `DEX_CONNECTOR_CONFIG_FILE` at the local mode-0600 `connections.json` written by Dex Web. From `connectors/zendesk/support/typescript`, run:

```sh
npm run example:ticket-changed
```

The first poll records a baseline and delivers no existing ticket. A Dex Web project keeps the poll cursor in process memory, so a local restart records a new baseline; a hosted project keeps it in project storage. `ZENDESK_LOCAL_API_BASE_URL` points the Worker at a local Zendesk-compatible stand-in for verification only. `LOG_LEVEL=debug` shows every delivery.

`flow/workflow.ts` declares the binding, `app.ts` routes it into the Flow through the binding's durable inbox and runs the Worker, and `main.ts` is the entrypoint. Run the real Dex integration tests against the stateful export fake (baseline, created, updated, and deleted delivery across a restart after a 429, and a 422 cursor rejection restarting without a gap):

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

Live Zendesk polling remains unverified.
