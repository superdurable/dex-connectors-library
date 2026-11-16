# Zendesk Support Python connector

This package implements `searchTickets`, `getTicket`, `createTicket`, and `updateTicket`, and the `ticketChanged` poll Trigger, for Dex Python applications. The manifest's Python version is `v0.2.0` and requires `dex-connectors-sdk` 0.12.0 or later; the Go connector remains on its independent release version.

Use the [read-ticket example](examples/read_ticket/flow.py) to bind the generated `new_get_ticket_step` factory to the static `zendesk-support-desk` connection. Its [Worker](examples/read_ticket/worker.py) loads that connection from Dex project configuration, and the [integration test](examples/read_ticket/integration_test.py) verifies the same Flow on a real Dex Worker and Client against a local provider stand-in. For provider behavior, configure a user-controlled Zendesk connection in Dex Web; no live Zendesk authorization was verified here.

Install this package in the Python application, run `uv sync --group dev`, then `uv run pytest` from this directory. The conformance suite replays Go-recorded requests and outcomes for all four operations.

Provider response parsing follows the Go connector for nullable optional fields, replay headers, and error tokens. Email inputs use the Go connector's bare-address grammar; timestamps require an explicit RFC 3339 offset and a UTC instant representable by Python `datetime` (years 1 through 9999).

## ticketChanged Trigger

`ticketChanged` reads Zendesk's cursor-based incremental ticket export at parity with the Go connector, as the [connector README](../README.md#ticketchanged-trigger) describes: a baseline that delivers no existing ticket, `created`, `updated`, and (with `includeDeleted`) `deleted` changes, a restart by `start_time` when Zendesk rejects a cursor, a 429 pause for `Retry-After`, and `per_page` halving for oversized pages. Its cursor documents and event IDs equal Go's, so Go and Python replicas share one binding; `tests/fixtures/go-ticket-export-cases.json`, recorded from the Go fetcher, pins requests, cursors, event IDs, and payloads. The connection must authenticate as a Zendesk administrator.

The [ticket change example](examples/ticket_changed/README.md) wires the saved binding into a Flow:

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

`new_project_ticket_trigger_runner` loads each binding's saved configuration and records every change in the binding's project Trigger inbox before its cursor commits in the project poll cursor. `new_ticket_trigger_runner` takes targets and cursor stores directly, and `new_ticket_changed_trigger` keeps its cursor in memory, so it records a new baseline on every start. Every binding of one `Client` shares its export request spacing: 10 requests per minute by default, or `Client(..., incremental_export_requests_per_minute=30)` for a High Volume account. `pollInterval` must be at least 5s.
