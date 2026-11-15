# Asana Python connector

`dex-connector-asana` v0.2.0 provides typed Dex Steps for `listTasks`,
`getTask`, `createTask`, `updateTask`, and `addComment`, and the `taskChanged`
poll Trigger. Install it with `uv add dex-connector-asana==0.2.0`; it requires
`dex-connectors-sdk` 0.12.0 or newer.

The package's generated `Config`, `Credentials`, operation models, Step
factories, connection constructors, and mock answers follow
[`connector.yaml`](../connector.yaml). Application provider calls run inside
the operation-specific Step Execute handlers. Asana has no idempotency key:
`createTask` and `addComment` use Dex heartbeat checkpoints to prevent a
second POST after an ambiguous outcome, then read back a known object ID.

Use the [approved-task example](examples/approved_task/README.md) to run a
Worker with the static `asana-tasks` connection. It exercises all five
operations through a real Dex Worker and Client in its integration test. The
[task-changed example](examples/task_changed/README.md) starts one Flow per
task event of a watched project through the `taskChanged` Trigger.
The [connector README](../README.md) covers token setup, branches, and the
provider behavior that remains unverified live.

## taskChanged Trigger

`taskChanged` polls one project's Asana Events API stream,
`GET /events?resource=<project gid>&sync=<token>`, on the SDK's durable poll
Trigger kit (`dex_connectors.sdk.pollingtrigger`), at parity with the Go
connector. The [connector README](../README.md#taskchanged-trigger) describes
the baseline, payload, expired tokens, rate limits, and delivery guarantees;
the Python package keeps every one of them:

- The first poll sends no sync token; Asana's 412 carries the baseline token,
  so existing tasks are never delivered.
- Each poll follows `has_more` through at most 10 pages of 100 events and keeps
  only task events; story, section, and project events are dropped.
- A 412 for a stored token, an undecodable cursor, or a cursor for another
  project reseeds and logs `poll cursor invalid; reseeding` at WARN. A 429
  pauses every binding of the connection for `Retry-After`; other failures back
  off per binding. None of them moves the cursor.
- The cursor token is `{"projectId":"<gid>","sync":"<token>"}`, and each Trigger
  event ID is `asana-` plus 32 hex digits of a SHA-256 over the requested sync
  token and the event's immutable facts. Both match Go byte for byte, so Go and
  Python replicas share one binding's poll cursor and Trigger inbox.
- The connection's `pollInterval` (Go nanoseconds in the saved configuration,
  `Config.poll_interval_seconds` in Python) is the delay between polls, at
  least 5 seconds; `Client` refuses a shorter one.

The payload is `TaskChangedEvent`; `TaskChangeAction` names its five `action`
values. `new_project_task_trigger_runner` is the durable path: it loads each
binding's saved project, records each event in the binding's project Trigger
inbox, and commits the sync token in the binding's project poll cursor only
after that. It returns a `PollRunner`; pass it the connection your Flow uses
and close that connection after the runner stops. From
[`examples/task_changed/main.py`](examples/task_changed/main.py):

```python
target = new_dex_flow_trigger_target(
    client,
    flow,
    accepts_event=accept_every_task_change,
    resolve_flow_id=resolve_flow_id,
    map_to_flow_input=map_to_flow_input,
)
# Each task event is recorded in the binding's project Trigger inbox before the sync token moves past it.
return new_project_task_trigger_runner(
    project,
    connection,
    ProjectTaskTriggerRunnerConfig(
        [ProjectTaskChangedTriggerRoute(TASK_CHANGED_TRIGGER_BINDING, target)]
    ),
)
```

`new_task_trigger_runner` takes targets and cursor stores directly, and
`new_task_changed_trigger` builds one binding's `TriggerRunner` with its cursor
in memory, so it records a new baseline on every start; use it only for tests
and demonstrations. Under `dexcli dev` the project poll cursor also stays in
process memory; hosted project storage keeps it. A binding without a saved
project, or with an invalid one, raises `ValueError`.

A Flow declares the binding with `define_task_changed_trigger_binding` in a
`get_connector_trigger_bindings` method whose body is one `return [...]`. The
[example Flow](examples/task_changed/flow.py) passes the `projectPicker` unit
as a `ConnectorConfigurationUI`. Dex CLI v1.47.3 reads the binding but reports
`connector_configuration_ui` for that dataclass literal, so Dex Web shows the
binding without the picker. Until the Python analyzer reads it, save the
binding's configuration, such as `{"projectId": "1201000000000001"}`, in the
`triggerBindings` entry of the project's `connections.json`.

## Verification

From this directory, run `uv sync --locked`, `uv run pytest tests`, and, with
`dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest
integration`. `tests/test_triggers.py` checks the fetcher against a local
Events API stand-in, including Go-computed event IDs and cursor tokens, and
`integration/test_task_changed.py` runs the taskChanged example against real
Dex: a baseline that starts no Flow, one Flow per task event, no Flow for a
comment story, delivery across a restart whose first poll answers 429, and an
expired sync token that records a new baseline without delivering the gap.
