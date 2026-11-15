# Gorgias ticket-changed example

This example starts one `GorgiasTicketChange` Flow for every ticket event in
the helpdesk's event log, such as `ticket-created`, `ticket-updated`, or
`ticket-closed`, through the `ticketChanged` Trigger:

1. `RecordTicketChange` validates the delivered event and records it.
2. `ReadChangedTicket` calls `gorgias.NewGetTicketStep`, so the record holds
   the ticket's status, tags, customer, and latest messages, which the event
   names only by ticket ID.
3. `CompleteTicketChange` records the ticket read back and completes the Flow.

The Flow ID is `gorgias-ticket-change-<Trigger event ID>`, and the Trigger
event ID is `gorgias-event-<Gorgias event ID>`. A redelivered event therefore
finds its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A missing, rejected, or invalid read fails the Flow.

The first start records Gorgias's current time as a baseline and starts no
Flow for earlier events. See the
[connector README](../../../README.md#ticketchanged-trigger) for event types,
the overlap window, paging, rejected cursors, and rate limits.

## Configure

Follow the [Gorgias setup](../../../README.md#gorgias-setup), then open
**Connections** in Dex Web and configure `gorgias / gorgias-ticket-watcher`:
the helpdesk domain, the user's email, and the user's API key. That user's role
decides which events and tickets the Trigger and the Flow can read.

- **pollInterval** on the connection is the delay between polls, at least 10
  seconds. A quiet poll makes one `GET /api/events` request, which counts
  against the account's 40 API-key requests per 20-second window.
- The `helpdesk-ticket-events` Trigger binding has no picker, because Studio
  setup commands cannot send a Gorgias API key over HTTP Basic to a per-account
  host. Its optional `eventTypes` configuration, such as
  `{"eventTypes": ["ticket-created", "ticket-closed"]}`, is saved under the
  binding in the project configuration. Unsaved, it delivers every ticket event
  type the Trigger reads (`gorgias.TicketEventTypes()`).

The Worker reads the saved values once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/gorgias/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/ticket-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/ticket-changed
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release. Run `dexcli dev` with that build
directory and the release override that the
[triage-issue example](../triage-issue/README.md#configure-and-run) builds, then run the Worker.
It reads the `DEX_PROJECT_*` project configuration, or the
`DEX_CONNECTOR_CONFIG_FILE` connections file that Dex Web writes under
`dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/ticket-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. For local verification
against a Gorgias-compatible fake only, `GORGIAS_LOCAL_API_BASE_URL` replaces
`https://{domain}.gorgias.com/api`; production Workers leave it unset.

With hosted project storage, each event is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats an event. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Events recorded while it
was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/ticket-changed/...
GOWORK=off go test -tags=integration ./examples/ticket-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Gorgias's event log and ticket read. They check that the baseline
starts no Flow, that each ticket event starts exactly one Flow, and that events
recorded while the Trigger process is stopped arrive after a restart. They also
check that a 429 waits for `Retry-After` before delivering, and that a 400
backs off and then delivers the event recorded meanwhile, from the same
cursor. Live
Gorgias polling has not been verified.
