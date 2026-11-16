# Zoho Desk TypeScript connector

Dex-native Zoho Desk searches, ticket reads, ticket creation, updates, and comments, plus the `ticketChanged` poll Trigger. The package uses generated OAuth refresh wiring for all nine Zoho data centers and pins `@superdurable/connectors-sdk` 0.12.0, whose durable poll Trigger kit runs the Trigger; it requires `@superdurable/dex` 1.45.4 or newer.

The [runnable search Flow](examples/search-tickets/README.md) uses the static `zoho-desk-helpdesk` connection. Its integration test runs the generated mock through a real Dex Worker and Client. Provider replay compares requests and outcomes with Go recordings; live Zoho authorization and provider behavior remain unverified.

## ticketChanged Trigger

`ticketChanged` delivers one `TicketChangedEvent` for every ticket created or changed in the connection's organization, optionally in one department, at parity with the Go connector: it polls `GET /api/v1/tickets/search` with `modifiedTimeRange` and `sortBy=modifiedTime`, 100 tickets per request and at most 10 requests per poll. The [connector README](../README.md#ticketchanged-trigger) describes the baseline, cursor, search index delay, clock skew, paging, change kinds, event IDs, rate limits, and resets; they apply unchanged to this package.

The cursor token and its keyed snapshot of up to 10,000 recent ticket versions are the Go connector's documents byte for byte, so Go and TypeScript replicas can share one binding. `test/fixtures/go-ticket-change-cases.json`, recorded from the Go fetcher, covers seeds (quiet, recent tickets, two pages, a local clock ahead of or behind Zoho Desk's `Date`), steady polls, the search index delay, a ticket listed twice, paging by modified time and by offset, the offset limit, every undecodable or foreign cursor, 429 with and without `Retry-After`, the 400 and 422 cursor rejections, and other failures. `test/ticket_change_fetcher.test.ts` replays every case and compares each request, cursor token, snapshot entry, event ID, and payload.

The [ticket change example](examples/ticket-changed/README.md) wires the Trigger into a Flow:

```ts
const runner = newProjectTicketTriggerRunner(project, ConnectionName, { ticketChangedRoutes: [{
  bindingName: TicketChangedTriggerBinding,
  target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
}] }, zohoDeskOptions);
```

- `newProjectTicketTriggerRunner(project, connectionName, config, options)` opens the connection, loads each binding's saved `TicketChangedTriggerConfiguration` (an unsaved binding watches every department), and records each change in the binding's project Trigger inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
- `newTicketTriggerRunner({ connection, ticketChangedRoutes })` takes explicit routes with their own configuration, target, and cursor store; a route without a cursor store keeps it in memory.
- `newTicketChangedTrigger(...)` is the equivalent of Go's generated `NewTicketChangedTrigger`: one binding with an in-memory cursor, so every start records a new baseline.
- `defineTicketChangedTriggerBinding(...)` declares the binding in a Flow's `getConnectorTriggerBindings`, optionally with the `departmentPicker` unit (`UIUnitDepartmentPicker`).

Each runner throws for a mock connection, no routes, a blank or duplicated binding name, a missing target, or a `departmentId` that is not a numeric Zoho Desk ID. `ZohoDeskClient` rejects a `pollIntervalMs` under 15 seconds, as Go does, and the project connection reads the saved `pollInterval` as Go nanoseconds. The client's `logger` option receives the runner's and the durable inboxes' records.

Live Zoho Desk polling, the search index delay, and tie order within one millisecond are unverified; the Trigger was verified against a local stand-in for Zoho Desk ticket search through a real Dex Worker and Client.
