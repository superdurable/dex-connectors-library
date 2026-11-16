# Google Calendar event-changed example for Python

This example starts one `GoogleCalendarEventChangePython` Flow for every event
created, updated, or cancelled on the picked calendar, through the
`eventChanged` Trigger:

1. `RecordEventChange` validates the delivered change and records it. A
   cancelled event completes the Flow at once, because Google guarantees only
   its ID.
2. `ReadChangedEvent` calls `new_get_event_step`, so the record holds the
   event's current state rather than the sync page's copy.
3. `CompleteEventChange` records the event read back and completes the Flow.

The Flow ID is `google-calendar-event-change-<Trigger event ID>`, and the
Trigger event ID is stable per event version, so a redelivered change finds its
existing Flow. Only the happy-path `found` branch is wired; a rejected or
invalid read fails the Flow. The first start records the calendar's existing
events as a baseline and starts no Flow for them.

## Configure

From `connectors/google/calendar/python`, generate the Flow's strict FDG 2.0
reference, which carries the static binding, and start `dexcli dev` with it:

```sh
uv run dexcli visualize ./examples/event_changed/flow.py \
  --schema-version 2.0 --json --out ./build/event-changed
dexcli dev -flow-rendering-dir ./build/event-changed
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

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.event_changed.main
```

[`routing.py`](routing.py) wires the Trigger to the Flow:

```python
route = ProjectEventChangedTriggerRoute(
    EVENT_CHANGED_TRIGGER_BINDING, new_event_change_target(dex_client, flow)
)
return new_project_event_trigger_runner(project, connection, [route])
```

[`main.py`](main.py) runs the Worker and the poll runner on one event loop until
interrupted. Trigger records, without event payloads or credentials, go to the
`dex_connectors.sdk.trigger` logger at INFO.

## Test

`uv run pytest tests/test_event_changed_example.py` checks the mapping,
validation, and static binding. With `dexcli dev` running, the integration test
drives the example target against a fake sync feed and a real Dex Worker and
Client: existing events are a baseline, each later change starts one Flow, a
change made while the process is stopped arrives after a restart, no change is
delivered twice, and an expired sync token records a new baseline without
delivering the gap:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/event_changed/integration_test.py
```

Delivery from a live Google Calendar has not been verified.
