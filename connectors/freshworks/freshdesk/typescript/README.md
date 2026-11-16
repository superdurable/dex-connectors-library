# Freshdesk TypeScript Connector

`@superdurable/connector-freshdesk` supplies Dex Step factories for five Freshdesk API v2 operations: `searchTickets`, `getTicket`, `createTicket`, `updateTicket`, and `addNote`. The package uses the same connection, operation, branch, and Trigger identities as the Go connector, and from 0.2.0 implements the `ticketChanged` poll Trigger on `@superdurable/connectors-sdk` 0.12.0. See the [connector README](../README.md) for Freshdesk setup, statuses, limits, and duplicate safety.

Create a Dex Web connection named `freshdesk-helpdesk`. Set `domain` to the lowercase part before `.freshdesk.com`, and save the agent's API key in the secret `api_key` field. Keep the key out of Flow input. `newProjectConnection` loads that static connection at Worker startup and resolves the current credential for each operation.

The runnable [ticket search example](examples/search-tickets/README.md) uses `newSearchTicketsStep` inside a Flow. Its `searched` branch is wired; the optional `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow if selected. The other generated factories are `newGetTicketStep`, `newCreateTicketStep`, `newUpdateTicketStep`, and `newAddNoteStep`.

## ticketChanged Trigger

`ticketChanged` starts a Flow for each Freshdesk ticket created or updated after the binding first started. It polls List All Tickets, `GET /api/v2/tickets` with `updated_since`, `order_by=updated_at`, `order_type=asc`, and `per_page=100`, on `@superdurable/connectors-sdk/pollingtrigger` at parity with the Go connector; the [connector README](../README.md#ticketchanged-trigger) describes the baseline, the one-minute overlap window, change kinds, paging, rate limits, and the rejected-cursor reseed. Each delivery is a `TriggerEvent<TicketChangedEvent>` whose ID is `freshdesk-ticket-<id>-<updated_at Unix seconds>`; `changeKind` is `created` or `updated`. Read the ticket with `getTicket` for its description, requester, and conversations, which the list omits.

The cursor token is the Go connector's JSON document byte for byte, and inbox payloads decode in either language, so Go and TypeScript replicas can share one binding. `test/ticket_change_fetcher.test.ts` replays fetches the Go connector recorded and compares every request, cursor token, and event ID.

`newProjectTicketTriggerRunner` loads each saved binding's configuration (an unsaved binding delivers every change kind) and records every change in the binding's project Trigger inbox before its cursor commits. From [`examples/ticket-changed/app.ts`](examples/ticket-changed/app.ts):

```ts
return newProjectTicketTriggerRunner(project, connection, {
  ticketChangedRoutes: [{ bindingName: TicketChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTicketChange, resolveFlowId, mapToFlowInput) }],
});
```

The runner takes the connection from `newProjectConnection`, whose `pollInterval` (at least 10 seconds) sets the delay between polls and whose `logger` client option receives the runner's and inboxes' records. `newTicketTriggerRunner` takes explicit routes and cursor stores; a route without a cursor keeps it in memory. `newTicketChangedTrigger` is the equivalent of Go's generated `NewTicketChangedTrigger`: it keeps its cursor in memory, so it records a new baseline on every start. A Flow declares the binding for Dex Web with `defineTicketChangedTriggerBinding`; the binding's only configuration is the optional `changeKinds` list. Differences from Go: durations are milliseconds (`TicketChangeOverlapMs`, `pollIntervalMs`), the project runner takes an open connection instead of a connection name and options, and timestamps keep JavaScript's millisecond precision.

## Validation

`createTicket` requester email and `searchTickets` requester email must be bare addresses with no dot at the start or end of the local part. A refused provider address is retried as an unsent request. `getTicket` and `updateTicket` receipts identify the requested ticket on provider response branches, including `notFound` and `invalidResponse`.

Run package checks with `npm ci && npm run check`; until `@superdurable/connectors-sdk` 0.12.0 is published, install a local build of `sdkts` first. The Go-recorded fixture in `../../../../conformance/connectors/freshworks/freshdesk/scenarios.json` covers all five operations, including branches, retry classifications, request shapes, and unkeyed write uncertainty. Run the examples' integration tests with `DEX_FLOW_SERVICE_ADDRESS` set to a local `dexcli dev` gRPC address; the search test's stand-in provider binds a free loopback port, and the [ticket change example](examples/ticket-changed/README.md)'s tests poll a stateful ticket-list fake through the baseline, one Flow per change across a restart, a 429 with `Retry-After`, and a rejected-cursor reseed.

Live Freshdesk authorization and provider behavior, including the `ticketChanged` polling behavior the connector README lists, have not been verified. The local integration test uses a Freshdesk-compatible HTTP stand-in through a real Dex Worker and Client.
