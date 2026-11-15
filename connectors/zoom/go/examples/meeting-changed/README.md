# Zoom meeting-changed example

This example starts one `ZoomMeetingChange` Flow for every meeting the
authorized Zoom user schedules, changes, or that leaves Zoom's scheduled list,
through the `meetingChanged` Trigger:

1. `RecordMeetingChange` validates the delivered change and records it. A
   `removed` meeting completes the Flow at once, because Zoom no longer lists
   it.
2. `ReadChangedMeeting` calls `zoom.NewGetMeetingStep`, so the record holds the
   full agenda and settings rather than the listed summary.
3. `CompleteMeetingChange` records the meeting read back and completes the Flow.
   The `notFound` branch is also wired here: a meeting deleted after the poll
   listed it is recorded without a current state instead of failing the Flow.

The Flow ID is `zoom-meeting-change-<Trigger event ID>`, and the Trigger event
ID is stable per change. A redelivered change therefore finds its existing Flow
instead of starting a second one. A rejected or invalid read fails the Flow.

The first start records the user's existing meetings as a baseline and starts
no Flow for them. See the
[connector README](../../../README.md#meetingchanged-trigger) for change kinds,
recurring meetings, page tokens, and rate limits.

## Configure

Follow the [Zoom setup](../../../README.md#zoom-setup), then authorize the
`zoom-meeting-watcher` connection under **Connections** in Dex Web. The
Trigger reads meetings with the `meeting:read:list_meetings` scope the
connection already requests, and the Flow reads each one with
`meeting:read:meeting`.

- **Poll interval** on the connection is the delay between polls, at least 30
  seconds. List meetings is a Medium API, which Zoom limits to 2,000 requests a
  day on Free accounts, so keep at least one minute there.
- The `host-meeting-changes` Trigger binding has no field: it always watches the
  authorized user's own meetings.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/zoom/go` with the latest stable dexcli
release:

```bash
mkdir -p build
dexcli visualize ./examples/meeting-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/meeting-changed
```

Run `dexcli dev` with that build directory, open Dex Web at
`http://127.0.0.1:8802` rather than `localhost`, then run the Worker. It reads
the `DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/meeting-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the meeting snapshot commits, so a restarted Worker neither misses
nor repeats a change. Under `dexcli dev`, the poll cursor stays in process
memory, so a restarted local Worker records a new baseline. Changes made while
it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/meeting-changed/...
GOWORK=off go test -tags=integration ./examples/meeting-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Zoom's List meetings and Get a meeting APIs. They check that the
baseline starts no Flow, that each scheduled, rescheduled, and deleted meeting
starts exactly one Flow, and that changes made while the Trigger process is
stopped arrive after a restart. They also check that a listing of another Zoom
user's meetings records a new baseline without delivering the gap. Set
`DEX_FLOW_SERVICE_ADDRESS` when the server is not at `127.0.0.1:8801`. Live
Zoom polling has not been verified.
