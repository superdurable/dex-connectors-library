# Zoho Desk Python connector

Dex-native Zoho Desk searches, ticket reads, ticket creation, updates, and comments, plus the `ticketChanged` poll Trigger. The package uses the generated OAuth refresh declaration for all nine Zoho data centers and requires `dex-connectors-sdk>=0.12.0`, whose durable poll Trigger kit runs the Trigger.

The runnable [search example](examples/search_tickets/README.md) uses the static `zoho-desk-helpdesk` connection. Its integration test runs the generated mock through a real Dex Worker and Client. Provider replay compares requests and outcomes with Go recordings; live Zoho authorization and provider behavior remain unverified.

## ticketChanged Trigger

`ticketChanged` delivers one `TicketChangedEvent` for every ticket created or changed in the connection's organization, optionally in one department, at parity with the Go connector: it polls `GET /api/v1/tickets/search` with `modifiedTimeRange` and `sortBy=modifiedTime`, 100 tickets per request and at most 10 requests per poll. The [connector README](../README.md#ticketchanged-trigger) describes the baseline, cursor, search index delay, clock skew, paging, change kinds, event IDs, rate limits, and resets; they apply unchanged to this package.

The cursor token and its keyed snapshot of up to 10,000 recent ticket versions are the Go connector's documents byte for byte, so Go and Python replicas can share one binding. `tests/fixtures/go-ticket-change-cases.json`, recorded from the Go fetcher, covers seeds (quiet, recent tickets, two pages, a local clock ahead of or behind Zoho Desk's `Date`), steady polls, the search index delay, a ticket listed twice, paging by modified time and by offset, the offset limit, every undecodable or foreign cursor, 429 with and without `Retry-After`, the 400 and 422 cursor rejections, and other failures. `tests/test_ticket_change_fetcher.py` replays every case and compares each request, cursor token, snapshot entry, event ID, and payload.

The [ticket change example](examples/ticket_changed/README.md) wires the Trigger into a Flow:

```python
target = new_dex_flow_trigger_target(
    dex_client,
    flow,
    accepts_event=accepts_every_ticket_change,
    resolve_flow_id=resolve_ticket_change_flow_id,
    map_to_flow_input=map_to_ticket_change,
)
route = ProjectTicketChangedTriggerRoute(TICKET_CHANGED_TRIGGER_BINDING, target)
return new_project_ticket_trigger_runner(
    project, connection, ProjectTicketTriggerRunnerConfig([route], logger=logger)
)
```

- `new_project_ticket_trigger_runner(project, connection, config)` loads each binding's saved `TicketChangedTriggerConfiguration` (an unsaved binding watches every department) and records each change in the binding's project Trigger inbox before its cursor commits in the binding's project poll cursor, so a restarted or replaced replica neither misses nor repeats a change.
- `new_ticket_trigger_runner(TicketTriggerRunnerConfig(connection, routes))` takes explicit `TicketChangedTriggerRoute` values with their own configuration, target, and cursor store; a route without a cursor store keeps it in memory.
- `new_ticket_changed_trigger(...)` is the equivalent of Go's generated `NewTicketChangedTrigger`: one binding with an in-memory cursor, so every start records a new baseline.
- `define_ticket_changed_trigger_binding(...)` declares the binding in a Flow's `get_connector_trigger_bindings`, optionally with the `departmentPicker` unit.

Each runner raises `ValueError` for a mock connection, no routes, a blank or duplicated binding name, a missing target, or a `departmentId` that is not a numeric Zoho Desk ID. `Client` rejects a `pollInterval` under 15 seconds, as Go does.

Live Zoho Desk polling, the search index delay, and tie order within one millisecond are unverified; the Trigger was verified against a local stand-in for Zoho Desk ticket search through a real Dex Worker and Client.
