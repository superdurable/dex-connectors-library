# Zendesk ticket change Flow

`ZendeskTicketChangePython` starts once per Zendesk ticket created, updated, or
deleted, through the `ticketChanged` poll Trigger. It records the delivered change,
completes at once for a deleted ticket, and otherwise reads the ticket back with
`getTicket` and its latest comment, then completes with a `TicketChangeOutcome`.

1. From `connectors/zendesk/support/python`, generate the strict FDG 2.0 graph:
   `mkdir -p build && uv run dexcli visualize ./examples/ticket_changed/flow.py --schema-version 2.0 --json --out ./build/ticket-changed`.
2. Run `dexcli dev -dex-port 37201 -web-port 37202 -open=false -flow-rendering-dir ./build`.
3. In Dex Web Connections, create the `zendesk-ticket-watcher` Zendesk Support
   connection with the account `subdomain`, an administrator's `email`, and that
   administrator's `api_token`, as the [connector README](../../../README.md#zendesk-setup)
   describes; Zendesk allows the incremental ticket export only for administrators.
   `pollInterval` is optional.
4. Optionally save the `zendesk-ticket-changes` binding's configuration as
   `{"includeDeleted": true}` to also receive deletions; unsaved, it delivers created
   and updated tickets. Zendesk has no setup bundle, so the binding has no
   configuration unit.
5. Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201 uv run python -m examples.ticket_changed.main`.

The first poll records a baseline and delivers no existing ticket. A Dex Web project
keeps the poll cursor in process memory, so a local restart records a new baseline;
a hosted project keeps it in project storage. `ZENDESK_LOCAL_API_BASE_URL` points the
Worker at a local Zendesk-compatible stand-in for verification only.

`flow.py` declares the binding, `triggers.py` routes it into the Flow through the
binding's durable inbox, and `main.py` runs the Worker and the poll runner together.
`integration_test.py` runs against a real Dex server (`DEX_FLOW_SERVICE_ADDRESS`) with
the stateful `export_fake.py`: baseline, created, updated, and deleted delivery, a
restart after a 429, and a rejected cursor restarting without a gap. Live Zendesk
behavior is not verified.
