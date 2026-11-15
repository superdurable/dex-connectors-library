# Zoom Python connector

`dex-connector-zoom` v0.2.0 implements the five Zoom Meetings operations as Dex Queries and Mutations: `listMeetings`, `getMeeting`, `createMeeting`, `updateMeeting`, and `listPastMeetingParticipants`, and the `meetingChanged` poll Trigger. It requires `dex-connectors-sdk>=0.12.0`, which adds the poll Trigger kit, and an OAuth user connection. The manifest in [`../connector.yaml`](../connector.yaml) owns the authorization guide, credential fields, scopes, operation and Trigger definitions, defaults, and generated model contracts. This language package does not change the Go connector's release.

`Client` resolves credentials for each call. `new_project_connection` binds a named Dex Web connection through generated refresh and shared-credential wiring. Close the connection after its Worker stops. The `createMeeting` Mutation uses the SDK's durable single-dispatch fence because Zoom has no create idempotency key; an unconfirmed dispatch selects `uncertain` and must be reconciled before any new create.

## meetingChanged Trigger

`meetingChanged` delivers each meeting the authorized user schedules, changes, or that leaves Zoom's scheduled list, as a `MeetingChangedEvent` with `change_kind` `scheduled`, `updated` (with `previous_meeting`), or `removed` (with the last listed state). Each poll reads every page of `GET /users/me/meetings?type=scheduled` and diffs it with a keyed snapshot committed in the binding's poll cursor. The behavior matches the Go connector, which the [connector README](../README.md#meetingchanged-trigger) describes in full: the first poll records a baseline and delivers nothing, event IDs are stable across refetches, a recurring meeting tracks its next listed occurrence, an expired page token restarts the listing once, more than 100 changes commit 100 at a time, a cursor that is undecodable or holds another Zoom user's meetings reseeds with a WARN record, and every 429, including the daily limit, pauses the connection for `Retry-After`. The cursor token and snapshot entries are byte-compatible with Go, and inbox payloads use the Go JSON fields, so Go and Python replicas can share one binding; `tests/test_meeting_changed_go_parity.py` replays a Go recording to prove it.

The connection's `pollInterval` setting, `Config.poll_interval_seconds` in code, is the delay between polls; `Client` rejects one under 30 seconds. The Trigger reads with the `meeting:read:list_meetings` scope the connection already requests.

| Function | Use |
| --- | --- |
| `new_project_meeting_trigger_runner(project, connection, routes)` | Durable delivery from a `LoadedProject`: each route's saved binding configuration is loaded (an unsaved binding delivers every kind), and each change is recorded in the binding's project Trigger inbox before its snapshot commits. |
| `new_meeting_trigger_runner(connection, routes)` | One `PollRunner` for `MeetingChangedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `new_meeting_changed_trigger(connection=, binding_name=, target=)` | One in-memory `TriggerRunner`, like Go's generated `NewMeetingChangedTrigger`; it records a new baseline on every start. |
| `define_meeting_changed_trigger_binding(connection_name=, binding_name=)` | Declares the binding in a Flow's `get_connector_trigger_bindings`. Dex Web shows no field for it. |

`MeetingChangedTriggerConfiguration.change_kinds` narrows deliveries in code; skipped kinds still advance the snapshot. The [`meeting_changed` example](examples/meeting_changed/README.md) wires a Flow target in [`worker.py`](examples/meeting_changed/worker.py):

```python
runner = new_project_meeting_trigger_runner(
    project,
    connection,
    [
        ProjectMeetingChangedTriggerRoute(
            BINDING_NAME, new_meeting_change_flow_target(dex_client, flow)
        )
    ],
)
```

`PollRunner.run(TriggerContext(cancellation))` replays the inboxes, seeds, and polls until the cancellation fires, then raises `TriggerDeliveryAbortedError`.

## Examples and tests

The checked-in [`meeting_list` example](examples/meeting_list/README.md) provides the Worker entrypoint, start command, strict FDG 2.0 source, and a real Dex Worker/Client integration test. It uses the static connection name `zoom-meetings`. The [`meeting_changed` example](examples/meeting_changed/README.md) starts one Flow per meeting change on `zoom-meeting-watcher`. Run the package tests with `uv sync --locked && uv run pytest tests`. Provider conformance replays the Go recording in [`scenarios.json`](../../../conformance/connectors/zoom/scenarios.json).

Real Zoom OAuth authorization, live polling, and provider behavior are unverified. The Go connector README lists the exact live behaviors that still need author testing through Dex Web Connections.
