# Zoho Desk ticket change Flow

`ZohoDeskTicketChangePython` starts once per Zoho Desk ticket created or
changed in the watched department, through the `ticketChanged` poll Trigger:

1. `RecordTicketChange` validates the delivered change and records it.
2. `ReadChangedTicket` calls `new_get_ticket_step`, so the record holds the
   ticket's current state with its description and newest threads and
   comments, which the search page leaves out.
3. `CompleteTicketChange` records the ticket read back and completes the Flow
   with a `TicketChangeOutcome`.

The Flow ID is `zoho-desk-ticket-change-<Trigger event ID>`, and the Trigger
event ID is stable per ticket version, so a redelivered change finds its
existing Flow. Only the happy-path `found` branch is wired; a ticket deleted
before the read, or a rejected or invalid read, fails the Flow. The first
start records the tickets modified in the last five minutes as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#ticketchanged-trigger) for change kinds,
the search index delay, paging, and rate limits.

`flow.py` declares the Flow and the `desk-ticket-changes` binding with its
department picker, `triggers.py` routes the binding into the Flow through the
binding's durable inbox, and `main.py` is the Worker entrypoint that runs the
Worker and the poll runner together.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer from
`connectors/zoho/desk`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/ticket_changed/flow.py \
  --schema-version 2.0 --json --out ./python/build/ticket_changed
```

1. Follow the [Zoho Desk setup](../../../README.md#zoho-setup), then configure
   the `zoho-desk-ticket-watcher` connection in Dex Web Connections: `orgId`
   from the organization picker after Connect, and optionally `pollInterval`,
   the delay between polls, at least 15 seconds. Every poll spends Zoho Desk
   API credits.
2. Optionally save the `desk-ticket-changes` binding's watched department.
   Dex CLI v1.47.3 does not read the Python binding's `departmentPicker` unit
   yet, so enter `{"departmentId": "<department ID>"}` in the binding's
   `triggerBindings` entry of `connections.json`: the long number in the
   browser address bar when the department is open under Setup > General >
   Departments in Zoho Desk. Unsaved or blank, the binding watches every
   department the connection's agent can see.
3. Start a local Dex server, such as
   `dexcli dev -dex-port 37311 -web-port 37312 -open=false -flow-rendering-dir python/build`.
4. From `connectors/zoho/desk/python`, run `uv sync --locked`, then
   `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37311 uv run python -m examples.ticket_changed.main`.
   Restart it after changing the binding; a changed department records a new
   baseline. Under `dexcli dev` the poll cursor stays in process memory, so a
   restart also records a new baseline and changes made while it was stopped
   are not delivered; a hosted project keeps the cursor in project storage.

Override `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed.
`ZOHO_DESK_LOCAL_API_BASE_URL` points the Worker at a local Zoho
Desk-compatible fake for verification only.

The deterministic tests are `tests/test_ticket_changed_example.py`,
`tests/test_triggers.py`, and `tests/test_ticket_change_fetcher.py`. With
`dexcli dev` running,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:37311 uv run pytest examples/ticket_changed/integration_test.py`
runs the example's Flow and target against real Dex and `desk_search_fake.py`,
a local stand-in for Zoho Desk ticket search and ticket read: the baseline
starts no Flow, each ticket version starts exactly one Flow, the binding sees
only its department, changes made while the Trigger process is stopped arrive
after a restart whose first poll answers 429, and a 422 naming
`modifiedTimeRange` records a new baseline. Live Zoho Desk authorization and
search remain unverified.
