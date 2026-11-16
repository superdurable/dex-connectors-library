# Zendesk Support for Dex TypeScript

Install `@superdurable/connector-zendesk-support` and bind a static `zendesk-support` connection. The package implements `searchTickets`, `getTicket`, `createTicket`, and `updateTicket`, and, from 0.2.0, the `ticketChanged` poll Trigger on `@superdurable/connectors-sdk` 0.12.0. Operation-specific Step factories, models, definitions, and scripted mocks are generated from the connector manifest and Go models.

The runnable [customer-issue example](examples/customer-issue/README.md) searches one ticket page. Its Worker uses `newProjectConnection` to load the Zendesk subdomain and credential fields from Dex project configuration. Enter `email` and `api_token` in Dex Web Connections; the token stays in the credential store. All optional error branches remain unwired in the example, so they fail the Flow with their safe Failure kind.

Run `npm ci`, then `npm run check` from this directory. To verify the example through a real Dex Worker and Client, start `dexcli dev` and run `npm run integration:customer-issue`. The 46 checked-in Go recordings replay every operation's branches and retry classifications in `npm test`. Search filters truncate `updatedSince` to UTC seconds, and provider failures include only safe error tokens, never free-form message text. Live Zendesk authorization and API behavior have not been verified.

## ticketChanged Trigger

`ticketChanged` reads Zendesk's cursor-based incremental ticket export at parity with the Go connector, as the [connector README](../README.md#ticketchanged-trigger) describes: a baseline that delivers no existing ticket, `created`, `updated`, and (with `includeDeleted`) `deleted` changes, a restart by `start_time` when Zendesk rejects a cursor, a 429 pause for `Retry-After`, and `per_page` halving for oversized pages. Its cursor documents and event IDs equal Go's, so Go and TypeScript replicas share one binding; `test/fixtures/go-ticket-export-cases.json`, recorded from the Go fetcher, pins requests, cursors, event IDs, and payloads. The connection must authenticate as a Zendesk administrator.

The [ticket-changed example](examples/ticket-changed/README.md) wires the saved binding into a Flow, as [app.ts](examples/ticket-changed/app.ts) does:

```ts
return newProjectTicketTriggerRunner(project, connection, {
  ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput) }],
});
```

`newProjectTicketTriggerRunner` loads each binding's saved configuration and records every change in the binding's project Trigger inbox before its cursor commits in the project poll cursor. `newTicketTriggerRunner` takes targets and cursor stores directly, and `newTicketChangedTrigger` keeps its cursor in memory, so it records a new baseline on every start. Every binding of one `ZendeskClient` shares its export request spacing: 10 requests per minute by default, or `incrementalExportRequestsPerMinute: 30` for a High Volume account. `pollInterval` must be at least 5s, and the `logger` option receives the runner's records, which never carry ticket payloads, cursors, or credentials.

Regenerate from the repository root with `GOWORK=off go run ./cmd/connectorctl generate connectors/zendesk/support/connector.yaml`, then check drift with `GOWORK=off go run ./cmd/connectorctl generate --check connectors/zendesk/support/connector.yaml`.
