# Gorgias Python connector

`dex-connector-gorgias` v0.2.0 implements the six Gorgias ticket and customer operations declared in [`connector.yaml`](../connector.yaml) for Dex Python applications: search tickets, get a ticket, find a customer by email, create a ticket, update a ticket, and add an internal note or public email reply. It also implements the `ticketChanged` poll Trigger. It requires `dex-connectors-sdk>=0.12.0`, which adds the poll Trigger kit.

Use `new_project_connection(project, "gorgias")` to bind a Dex Web connection. The connection resolves the Gorgias email and API key on each provider call. `new_connection` accepts a configured `Client` for an application that supplies its own credential provider. Close an owned client after the Worker stops.

The runnable `examples/triage_issue` Flow exercises ticket search and detail lookup. For provider behavior, supply a connection through Dex Web and run the example against a real Gorgias account. The local integration test uses a provider stand-in and does not establish live provider authorization.

## ticketChanged Trigger

`ticketChanged` delivers each Gorgias ticket event, such as `ticket-created`, `ticket-updated`, or `ticket-closed`, recorded after the binding first started, as a `TicketChangedEvent` with `event_id`, `event_type`, `ticket_id`, `occurred_at`, `user_id` (zero for a rule), and `context`. Each poll reads `GET /api/events` in `created_datetime` order. The behavior matches the Go connector, which the [connector README](../README.md#ticketchanged-trigger) describes in full: the first poll records a baseline at Gorgias's `Date` header and delivers nothing, each poll rereads a 15-second overlap and skips events it has seen, it reads at most five pages of 100 events per poll and stops paging when `X-Gorgias-Account-Api-Call-Limit` shows three quarters of the window spent, a 429 pauses the connection for `Retry-After`, a 400 backs off and retries from the same position, and only an undecodable cursor records a new baseline. Event IDs are `gorgias-event-<id>`, stable across refetches. The cursor token is byte-compatible with Go, and inbox payloads use the Go JSON fields, so Go and Python replicas can share one binding; `tests/test_ticket_changed_go_parity.py` replays a Go recording to prove it.

The connection's `pollInterval` setting, `Config.poll_interval_seconds` in code, is the delay between polls; `Client` rejects one under 10 seconds.

| Function | Use |
| --- | --- |
| `new_project_ticket_trigger_runner(project, connection, config)` | Durable delivery from a `LoadedProject`: each route's saved binding configuration is loaded (an unsaved binding delivers every ticket event type), and each event is recorded in the binding's project Trigger inbox before its cursor commits. |
| `new_ticket_trigger_runner(config)` | One `PollRunner` for `TicketChangedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `new_ticket_changed_trigger(connection=, binding_name=, configuration=, target=)` | One in-memory `TriggerRunner`, like Go's generated `NewTicketChangedTrigger`; it records a new baseline on every start. |
| `define_ticket_changed_trigger_binding(connection_name=, binding_name=)` | Declares the binding in a Flow's `get_connector_trigger_bindings`. Dex Web shows no field for it. |

`TicketChangedTriggerConfiguration.event_types` narrows deliveries to some of `TICKET_EVENT_TYPES`; skipped events still advance the cursor. The [`ticket_changed` example](examples/ticket_changed/README.md) wires a Flow target in [`worker.py`](examples/ticket_changed/worker.py):

```python
runner = new_project_ticket_trigger_runner(
    project,
    connection,
    ProjectTicketTriggerRunnerConfig(
        [ProjectTicketChangedTriggerRoute(BINDING_NAME, new_ticket_change_flow_target(dex_client, flow))]
    ),
)
```

`PollRunner.run(TriggerContext(cancellation))` replays the inboxes, seeds, and polls until the cancellation fires, then raises `TriggerDeliveryAbortedError`. Live Gorgias polling has not been verified.

## Tests

Run `uv sync --group dev` and `uv run pytest tests` from this directory. The conformance test replays the Go connector's recorded provider scenarios.
