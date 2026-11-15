# Google Calendar Python connector

Install `dex-connector-google-calendar==0.2.0` alongside a compatible Dex Python SDK; it requires `dex-connectors-sdk>=0.12.0` for the poll Trigger kit. The package exposes `Client`, generated `Config` and `Credentials`, a generated project connection with Google OAuth and Workspace delegated refresh, operation-specific Step factories for `listEvents`, `getEvent`, `createEvent`, `updateEvent`, and `queryFreeBusy`, and the `eventChanged` Trigger.

See [`examples/calendar_events`](examples/calendar_events/README.md) for a runnable Flow with the static connection name `google-calendar-events-reader`. The example lists a bounded event window and persists the public result. It uses the generated project connection, so the Worker reads credentials from Dex project storage and never takes a token from Flow input.

## eventChanged Trigger

`eventChanged` polls one calendar with Google Calendar incremental sync and delivers one `EventChangedEvent` (`calendar_id`, `change_kind`, and the `event`) per created, updated, or cancelled event, at parity with the Go connector; the [connector README](../README.md#eventchanged-trigger) describes the baseline, change kinds, recurring events, expired sync tokens, and rate limits. The binding's sync cursor document and Trigger event IDs are byte for byte the Go connector's, and a change a Go replica recorded in the inbox replays in Python.

- `new_project_event_trigger_runner(project, connection, routes)` is the durable path: it loads each binding's saved calendar (an unsaved binding watches `primary`), records every change in the binding's project Trigger inbox, and commits the sync token in the binding's project poll cursor only after that. The caller opens and closes `connection`, usually with `new_project_connection`, and shares it with the Flow.
- `new_event_trigger_runner(connection, routes)` takes targets and cursor stores directly; a route without a cursor keeps it in memory.
- `new_event_changed_trigger(...)` is one binding's `TriggerRunner` with an in-memory cursor, as Go's generated `NewEventChangedTrigger` is. A memory cursor records a new baseline on every start, so use it only for tests and demonstrations.
- `define_event_changed_trigger_binding` declares the binding in a Flow's `get_connector_trigger_bindings`; the `calendarPicker` unit saves `calendarId` and `calendarName`.

The connection's `pollInterval` (at least 5 seconds) sets the delay between polls. A 429 or a Google usage-limit 403 pauses every binding of the connection, and a 410 Gone records a new baseline and logs a WARN record to the `dex_connectors.sdk.trigger` logger. Records carry binding identity, never event payloads or credentials.

[`examples/event_changed`](examples/event_changed/README.md) is the runnable Trigger example: the `calendar-event-changes` binding of the static `google-calendar-watcher` connection starts one `GoogleCalendarEventChangePython` Flow per change, which reads a created or updated event back with `new_get_event_step`.

## Tests

Run provider replay, the generated OAuth agreement cases, and the Trigger unit tests with `uv run pytest tests`. The example integration tests require a running `dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`: `uv run pytest examples/calendar_events/integration_test.py examples/event_changed/integration_test.py`.

Go-recorded provider fixtures and a real Dex stack with a local Google stand-in verify request, Trigger delivery, and Flow behavior. Live Google consent, delegated token minting against Google, Calendar API calls, and incremental sync against a live calendar remain unverified.
