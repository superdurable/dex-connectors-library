# Freshdesk ticket change Flow

`FreshdeskTicketChangePython` starts once per Freshdesk ticket created or updated,
through the `ticketChanged` poll Trigger. It records the delivered change, reads the
ticket back with `getTicket` for its description, requester, and first conversations,
and completes with a `TicketChangeOutcome`.

1. Run `dexcli dev -dex-port 37201 -web-port 37202 -open=false`.
2. In Dex Web Connections, create the `freshdesk-ticket-watcher` Freshdesk connection
   with the helpdesk `domain` and the agent's `api_key`, as the
   [connector README](../../../README.md#freshdesk-setup) describes. `pollInterval`
   is optional.
3. Optionally save the `helpdesk-ticket-changes` binding's configuration, for example
   `{"changeKinds": ["created"]}`; unsaved, it delivers every change. Freshdesk setup
   commands cannot send the HTTP Basic API key, so the binding has no picker units.
4. From `python/`, run
   `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37201 uv run python -m examples.ticket_changed.main`.

The first poll records a baseline and delivers no existing ticket. A Dex Web project
keeps the poll cursor in process memory, so a local restart records a new baseline;
a hosted project keeps it in project storage. `FRESHDESK_LOCAL_API_BASE_URL` points
the Worker at a local Freshdesk-compatible fake for verification only.

`flow.py` declares the binding, `triggers.py` routes it into the Flow through the
binding's durable inbox, and `main.py` runs the Worker and the poll runner together.
`integration_test.py` runs against a real Dex server
(`DEX_FLOW_SERVICE_ADDRESS`) with the stateful `ticket_list_fake.py`.
