# Google Calendar event-changed example for TypeScript

This example starts one `GoogleCalendarEventChangeTypeScript` Flow for every
event created, updated, or cancelled on the picked calendar, through the
`eventChanged` Trigger:

1. `RecordEventChange` validates the delivered change and records it. A
   cancelled event completes the Flow at once, because Google guarantees only
   its ID.
2. `ReadChangedEvent` calls `newGetEventStep`, so the record holds the event's
   current state rather than the sync page's copy.
3. `CompleteEventChange` records the event read back and completes the Flow.

The Flow ID is `google-calendar-event-change-<Trigger event ID>`, and the
Trigger event ID is stable per event version, so a redelivered change finds its
existing Flow. Only the happy-path `found` branch is wired; a rejected or
invalid read fails the Flow. The first start records the calendar's existing
events as a baseline and starts no Flow for them.

## Configure

Generate and lint its strict Flow Definition Graph 2.0 from
`connectors/google/calendar/typescript`:

```sh
npm run build
npm install --prefix examples/event-changed --no-save --no-package-lock --ignore-scripts .
cd examples/event-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Follow the [Google Calendar Connector setup](../../../README.md), then configure
the `google-calendar-watcher` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Shorter intervals deliver changes sooner and use more Calendar API
  quota.
- **Watched calendar** on the `calendar-event-changes` Trigger binding lists the
  calendars in this connection's Google Calendar list whose events it can read.
  It saves the calendar ID and name. Leave it unsaved to watch the authorized
  account's `primary` calendar.

The Worker reads the saved values once at startup, so restart it after saving.
A changed calendar records a new baseline. Under `dexcli dev` the poll cursor
stays in process memory, so a restarted local Worker also records a new
baseline; hosted project storage keeps it.

## Run

From `connectors/google/calendar/typescript`:

```sh
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:event-changed
```

[`app.ts`](app.ts) wires the Trigger to the Flow and runs the Worker and the
poll runner until interrupted. `LOG_LEVEL=debug` also shows every delivery;
records never carry event payloads or credentials.

## Test

`test/event_changed_workflow.test.ts` checks the mapping, validation, and static
binding. With `dexcli dev` running, `npm run test:integration` drives this
application against an in-process sync feed and a real Dex Worker and Client:
existing events are a baseline, each later change starts one Flow, a change
made while the process is stopped arrives after a restart, no change is
delivered twice, and an expired sync token records a new baseline without
delivering the gap.

Delivery from a live Google Calendar has not been verified.
