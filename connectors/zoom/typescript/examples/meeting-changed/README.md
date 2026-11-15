# Zoom meeting-changed example for TypeScript

This example starts one `ZoomMeetingChangeTypeScript` Flow for every meeting
the authorized Zoom user schedules, changes, or that leaves Zoom's scheduled
list, through the `meetingChanged` Trigger's `host-meeting-changes` binding:

1. `RecordMeetingChange` validates the delivered change and records it. A
   `removed` meeting completes the Flow at once, because Zoom no longer lists
   it.
2. `ReadChangedMeeting` calls `newGetMeetingStep`, so the record holds the full
   agenda and settings rather than the listed summary.
3. `CompleteMeetingChange` records the meeting read back and completes. The
   `notFound` branch is also wired: a meeting deleted after the poll listed it
   is recorded without a current state instead of failing the Flow.

The Flow ID is `zoom-meeting-change-<Trigger event ID>`, and the event ID is
stable per change, so a redelivered change finds its existing Flow. The first
start records the user's existing meetings as a baseline and starts no Flow
for them. See the [connector README](../../../README.md#meetingchanged-trigger)
for change kinds, recurring meetings, page tokens, and rate limits.

## Configure

Follow the [Zoom setup](../../../README.md#zoom-setup), then authorize the
`zoom-meeting-watcher` connection under **Connections** in Dex Web. The
Trigger reads meetings with the `meeting:read:list_meetings` scope the
connection already requests, and the Flow reads each one with
`meeting:read:meeting`.

- **Poll interval** on the connection is the delay between polls, at least 30
  seconds. List meetings is a Medium API, which Zoom limits to 2,000 requests a
  day on Free accounts, so keep at least one minute there.
- The `host-meeting-changes` binding has no field: it always watches the
  authorized user's own meetings.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/zoom/typescript`, build the package, make it resolvable from
the example, and generate strict FDG 2.0:

```bash
npm ci && npm run build
npm install --prefix examples/meeting-changed --no-save --no-package-lock --ignore-scripts .
cd examples/meeting-changed
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/meeting-changed --open=false
```

Run `dexcli dev -flow-rendering-dir ./build`, configure the connection in Dex
Web, set `DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` Dex Web wrote,
and start the Worker, which also runs the Trigger:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:meeting-changed
```

Set `LOG_LEVEL` to `debug`, `info`, `warn`, or `error`; Trigger records carry
no payloads or credentials.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the meeting snapshot commits, so a restarted Worker neither misses
nor repeats a change. Under `dexcli dev`, the poll cursor stays in process
memory, as in Go, so a restarted local Worker records a new baseline and
changes made while it was stopped are not delivered.

## Test

```bash
npx vitest run test/meeting_changed_workflow.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration/meeting-changed.integration.test.ts
```

The integration tests run `runMeetingChangeWorker` on a real Dex Worker and
Client against a fake of Zoom's List meetings and Get a meeting APIs, with
project storage shared by two processes. They check that the baseline starts no
Flow, that each scheduled, rescheduled, and deleted meeting starts exactly one
Flow, and that changes made while the process is stopped arrive after a
restart. They also check that a listing of another Zoom user's meetings records
a new baseline without delivering the gap. Live Zoom polling has not been
verified.
