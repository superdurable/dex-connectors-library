# Zoho Desk ticket-changed example

This example starts one `ZohoDeskTicketChange` Flow for every ticket created
or changed in the picked department, through the `ticketChanged` Trigger:

1. `RecordTicketChange` validates the delivered change and records it.
2. `ReadChangedTicket` calls `desk.NewGetTicketStep`, so the record holds the
   ticket's current state with its description and newest threads and
   comments, which the search page leaves out.
3. `CompleteTicketChange` records the ticket read back and completes the Flow.

The Flow ID is `zoho-desk-ticket-change-<Trigger event ID>`, and the Trigger
event ID is stable per ticket version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A ticket deleted before the read, or a rejected or invalid
read, fails the Flow.

The first start records the tickets modified in the last five minutes as a
baseline and starts no Flow for them. See the
[connector README](../../../README.md#ticketchanged-trigger) for change kinds,
the search index delay, paging, and rate limits.

## Configure

Follow the [Zoho Desk setup](../../../README.md#zoho-setup), then configure the
`zoho-desk-ticket-watcher` connection under **Connections** in Dex Web.

- **orgId** on the connection is the organization, chosen with the
  organization picker after Connect.
- **Poll interval** on the connection is the delay between polls, at least 15
  seconds. Every poll spends Zoho Desk API credits.
- **Watched department** on the `desk-ticket-changes` Trigger binding lists the
  enabled departments of the connection's organization from its own data
  center and saves the department ID and name. Leave it blank to watch every
  department the connection's agent can see.

The Worker reads the saved values once at startup, so restart it after saving.
A changed department records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/zoho/desk/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/ticket-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/ticket-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/ticket-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. `ZOHO_DESK_LOCAL_API_BASE_URL`
points the Worker at a local Zoho Desk-compatible fake for verification only.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/ticket-changed/...
GOWORK=off go test -tags=integration ./examples/ticket-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against
`internal/desksearchfake`, a local stand-in for Zoho Desk's ticket search and
ticket read. They check that the baseline starts no Flow, that each ticket
version starts exactly one Flow, that a binding sees only its department, and
that changes made while the Trigger process is stopped arrive after a restart.
They also check that a 422 naming `modifiedTimeRange` records a new baseline.
Live Zoho Desk search has not been verified.
