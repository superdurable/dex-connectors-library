# Freshdesk ticket-changed example

This example starts one `FreshdeskTicketChange` Flow for every ticket created
or updated in the helpdesk, through the `ticketChanged` Trigger:

1. `RecordTicketChange` validates the delivered change and records it.
2. `ReadChangedTicket` calls `freshdesk.NewGetTicketStep`, so the record holds
   the ticket's description, requester, and first conversations, which the
   ticket list omits.
3. `CompleteTicketChange` records the ticket read back and completes the Flow.

The Flow ID is `freshdesk-ticket-change-<Trigger event ID>`, and the Trigger
event ID is stable per ticket version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A missing, rejected, or invalid read fails the Flow.

The first start records Freshdesk's current time as a baseline and starts no
Flow for existing tickets. See the
[connector README](../../../README.md#ticketchanged-trigger) for change kinds,
the overlap window, paging, rejected cursors, and rate limits.

## Configure

Follow the [Freshdesk setup](../../../README.md#freshdesk-setup), then open
**Connections** in Dex Web and configure `freshdesk / freshdesk-ticket-watcher`:
the helpdesk domain and the agent's API key. The agent's role decides which
tickets the Trigger sees.

- **pollInterval** on the connection is the delay between polls, at least 10
  seconds. Each poll normally makes one List All Tickets request, which counts
  against the account's per-minute limit and its Tickets List sublimit.
- The `helpdesk-ticket-changes` Trigger binding has no picker, because Freshdesk
  setup commands cannot send the agent's HTTP Basic API key. Its optional
  `changeKinds` configuration, such as `{"changeKinds": ["created"]}`, is saved
  under the binding in the project configuration. Unsaved, it delivers created
  and updated tickets.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/freshworks/freshdesk/go` with the
latest stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/ticket-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/ticket-changed
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release. Run `dexcli dev` with that build
directory and the release override that the
[triage-issue example](../triage-issue/README.md#configure-and-run) builds,
then run the Worker. It reads the `DEX_PROJECT_*` project configuration, or the
`DEX_CONNECTOR_CONFIG_FILE` connections file that Dex Web writes under
`dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/ticket-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. For local verification
against a Freshdesk-compatible fake only, `FRESHDESK_LOCAL_API_BASE_URL`
replaces `https://{domain}.freshdesk.com/api/v2`; production Workers leave it
unset.

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

The integration tests run a real Dex Worker and Client against a local
stand-in for Freshdesk's ticket list. They check that the baseline starts no
Flow, that each change starts exactly one Flow, and that changes made while the
Trigger process is stopped arrive after a restart. They also check that a 429
waits for `Retry-After` before delivering, and that a 400 for the cursor's
query records a new baseline without delivering the gap. Live Freshdesk
polling has not been verified.
