# Ticket changed example

This Worker starts one `GorgiasTicketChangePython` Flow for every Gorgias ticket event, such as
`ticket-created` or `ticket-closed`, that the `ticketChanged` poll Trigger reads from the helpdesk's
event log. The Flow records the event, reads the ticket back with `getTicket` for its status, tags,
customer, and latest messages, and completes with that read-back state.

| Identity | Value |
| --- | --- |
| Flow type | `GorgiasTicketChangePython` |
| Connection | `gorgias-ticket-watcher` |
| Trigger binding | `helpdesk-ticket-events` |
| Flow ID | `gorgias-ticket-change-gorgias-event-<event id>` |
| Steps | `RecordTicketChange`, `ReadChangedTicket`, `CompleteTicketChange` |

## Configure

Create the `gorgias-ticket-watcher` connection in Dex Web Connections with the Gorgias domain, user
email, and API key that the [connector README](../../../README.md#gorgias-setup) describes. The
connection's `pollInterval` field sets the delay between polls. The binding has no Dex Web field; an
optional saved `eventTypes` configuration narrows the delivered event types.

## Run

Start a local Dex stack, then from `connectors/gorgias/python`:

```bash
uv sync --group dev
DEX_FLOW_SERVICE_ADDRESS=localhost:8801 uv run python -m examples.ticket_changed.worker
```

The first poll records a baseline and starts no Flow. Change a ticket in Gorgias, and within one poll
interval a Flow starts for each event the change recorded. Set `LOG_LEVEL=DEBUG` for more detail;
Trigger records, which never carry payloads or credentials, go to the `dex_connectors.sdk.trigger`
logger.

With hosted project storage, each event is recorded in the binding's Trigger inbox before the cursor
commits, so a restarted Worker neither misses nor repeats an event. Under `dexcli dev`, the poll cursor
stays in process memory, as in Go, so a restarted local Worker records a new baseline and events
recorded while it was stopped are not delivered.

## Test

```bash
uv run pytest tests/test_ticket_changed_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/ticket_changed/integration_test.py
```

The integration tests run a real Dex Worker and Client against a fake of Gorgias's list events and
get ticket APIs, with project storage shared by two Trigger processes. They check that the baseline
starts no Flow, that each later ticket event starts exactly one Flow that reads the ticket back, that
events recorded while the Trigger process is stopped arrive after a restart, and that a 429 and a 400
recover without a new baseline. Live Gorgias polling has not been verified.
