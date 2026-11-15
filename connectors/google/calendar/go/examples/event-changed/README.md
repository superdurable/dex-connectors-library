# Google Calendar event-changed example

This example starts one `GoogleCalendarEventChange` Flow for every event
created, updated, or cancelled on the picked calendar, through the
`eventChanged` Trigger:

1. `RecordEventChange` validates the delivered change and records it. A
   cancelled event completes the Flow at once, because Google guarantees only
   its ID.
2. `ReadChangedEvent` calls `calendar.NewGetEventStep`, so the record holds the
   event's current state rather than the sync page's copy.
3. `CompleteEventChange` records the event read back and completes the Flow.

The Flow ID is `google-calendar-event-change-<Trigger event ID>`, and the
Trigger event ID is stable per event version. A redelivered change therefore
finds its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A rejected or invalid read fails the Flow.

The first start records the calendar's existing events as a baseline and starts
no Flow for them. See the
[connector README](../../../README.md#eventchanged-trigger) for change kinds,
recurring events, expired sync tokens, and rate limits.

## Configure

Follow the [Google Calendar Connector setup](../../../README.md), then configure
the `google-calendar-watcher` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Shorter intervals deliver changes sooner and use more Calendar API
  quota.
- **Watched calendar** on the `calendar-event-changes` Trigger binding lists the
  calendars in this connection's Google Calendar list whose events it can read
  (`reader` access or above). It saves the calendar ID and name. Leave it
  unsaved to watch the authorized account's `primary` calendar.

The Worker reads the saved values once at startup, so restart it after saving.
A changed calendar records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/google/calendar/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/event-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/event-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/event-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the sync token commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/event-changed/...
GOWORK=off go test -tags=integration ./examples/event-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Google Calendar's sync protocol. They check that the baseline
starts no Flow, that each change starts exactly one Flow, and that changes
made while the Trigger process is stopped arrive after a restart. They also
check that a `410 Gone` records a new baseline without delivering the gap.
Live Google Calendar sync has not been verified.
