# Zendesk Support ticket-changed example

This example starts one `ZendeskTicketChange` Flow for every ticket created,
updated, or deleted in the Zendesk account, through the `ticketChanged`
Trigger:

1. `RecordTicketChange` validates the delivered change and records it. A
   deleted ticket completes the Flow at once, because Zendesk no longer serves
   it.
2. `ReadChangedTicket` calls `support.NewGetTicketStep` with one latest
   comment, so the record holds the ticket's current state rather than the
   export page's copy.
3. `CompleteTicketChange` records the ticket read back and completes the Flow.

The Flow ID is `zendesk-ticket-change-<Trigger event ID>`, and the Trigger
event ID is stable per ticket version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A missing, rejected, or invalid read fails the Flow.

The first start records the account's existing tickets as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#ticketchanged-trigger) for change kinds,
deletions, cursor resets, and rate limits.

## Configure

Follow the [Zendesk Support setup](../../../README.md#zendesk-setup), then
configure the `zendesk-ticket-watcher` connection under **Connections** in Dex
Web:

- **subdomain**, **email**, and **api_token** as the setup describes. The email
  must belong to a Zendesk administrator: the incremental ticket export answers
  403 to an agent, and the Worker then logs `poll failed; retrying`.
- **pollInterval** is the delay between polls, at least 5 seconds. All bindings
  of the connection share Zendesk's 10 export requests per minute.

The `zendesk-ticket-changes` Trigger binding has no fields: it watches created
and updated tickets in the whole account. To also receive deletions, save
`{"includeDeleted": true}` as the binding's configuration in the project. The
Worker reads the connection and binding once at startup, so restart it after
saving.

## Run

Generate strict FDG 2.0 from `connectors/zendesk/support/go` with the latest
stable dexcli release:

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
(`debug`, `info`, `warn`, or `error`) when needed. For local verification
against a Zendesk-compatible fake only, `ZENDESK_SUPPORT_LOCAL_API_BASE_URL`
replaces `https://{subdomain}.zendesk.com/api/v2`; it must be HTTPS or a
loopback HTTP URL, and production Workers leave it unset.

The Worker records each change in the binding's project Trigger inbox before
the export cursor commits, so a restarted or replaced Worker neither misses nor
repeats a change.

## Test

```bash
GOWORK=off go test ./examples/ticket-changed/...
GOWORK=off go test -tags=integration ./examples/ticket-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Zendesk's incremental ticket export. They check that the baseline
starts no Flow, that each created, updated, and deleted ticket starts exactly
one Flow, and that changes made while the Trigger process is stopped arrive
after a restart whose first request answers 429. They also check that a cursor
Zendesk rejects with 422 restarts by time without losing the change made in
between. Live Zendesk behavior has not been verified.
