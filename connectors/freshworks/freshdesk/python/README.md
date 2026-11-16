# Freshdesk Python connector

Install `dex-connector-freshworks-freshdesk==0.2.0`. The package implements
`searchTickets`, `getTicket`, `createTicket`, `updateTicket`, and `addNote` with
operation-specific Step factories, and the `ticketChanged` poll Trigger. It requires
`dex-connectors-sdk>=0.12.0`, the release with the durable poll Trigger kit.

Bind a `freshdesk` project connection through `new_project_connection`, or use
`Client` with a static credential provider in a local test. The connection needs
the helpdesk `domain` and secret `api_key` described in the [parent README](../README.md).
The optional `maxResponseBytes` setting bounds successful response bodies, and
`pollInterval` (at least 10 seconds) sets the delay between `ticketChanged` polls;
blank uses the manifest default. Credentials are resolved before every request.

## ticketChanged Trigger

`ticketChanged` starts a Flow for each Freshdesk ticket created or updated after the
binding first started. It polls List All Tickets, `GET /api/v2/tickets` with
`updated_since`, `order_by=updated_at`, `order_type=asc`, and `per_page=100`, on
`dex_connectors.sdk.pollingtrigger` at parity with the Go connector; the
[parent README](../README.md#ticketchanged-trigger) describes the baseline, the
one-minute overlap window, change kinds, paging, rate limits, and the rejected-cursor
reseed. Each delivery is a `TriggerEvent[TicketChangedEvent]` whose ID is
`freshdesk-ticket-<id>-<updated_at Unix seconds>`; `change_kind` is
`TICKET_CHANGE_CREATED` or `TICKET_CHANGE_UPDATED`. Read the ticket with `getTicket`
for its description, requester, and conversations, which the list omits.

The cursor token is the Go connector's JSON document byte for byte, and inbox
payloads decode in either language, so Go and Python replicas can share one binding.
`tests/test_ticket_change_fetcher.py` replays fetches the Go connector recorded and
compares every request, cursor token, and event ID.

`new_project_ticket_trigger_runner` loads each saved binding's configuration (an
unsaved binding delivers every change kind) and records every change in the binding's
project Trigger inbox before its cursor commits. From
[`examples/ticket_changed/triggers.py`](examples/ticket_changed/triggers.py):

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

The runner takes the connection from `new_project_connection`; the caller closes it
after the runner stops. `new_ticket_trigger_runner` takes explicit
`TicketChangedTriggerRoute` values and cursor stores; a route without a cursor keeps
it in memory. `new_ticket_changed_trigger` is the equivalent of Go's generated
`NewTicketChangedTrigger`: it keeps its cursor in memory, so it records a new baseline
on every start. A Flow declares the binding for Dex Web with
`define_ticket_changed_trigger_binding`; the binding's only configuration is the
optional `changeKinds` list, saved under the binding in the project configuration.

Differences from Go: logging goes through a `TriggerLogger` passed in the runner
configuration instead of the client's `WithLogger` option, the project runner takes
an open connection instead of a connection name and options, and inbox payloads store
object keys in sorted order instead of Go's struct order.

## Examples and verification

The checked-in [search example](examples/search_tickets/README.md) and
[ticket change example](examples/ticket_changed/README.md) run through a Dex Worker.
The 48 Go-recorded provider scenarios cover the five operations, including retries,
uncertainty, and heartbeat checkpoints. They are replayed by
`uv run pytest tests/test_conformance.py`. The search example's integration test uses
a real local Dex stack with the generated mock; the ticket change example's
integration tests poll a stateful ticket-list fake into a real Dex Worker and cover
the baseline, one Flow per change across a restart, a 429 with `Retry-After`, and a
rejected-cursor reseed. Live Freshdesk provider behavior, including the
`ticketChanged` polling behavior the parent README lists, and Dex Web authorization
have not been verified for this package.
