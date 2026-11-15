# Asana task-changed example

Every task added to, changed in, removed from, deleted from, or restored to the
watched project, subtasks included, starts one `AsanaTaskChangePython` Flow
through the `taskChanged` poll Trigger. The Flow records the delivered change,
reads the task back with `new_get_task_step` (Asana events name the change, not
the task's current state), and completes with both. A deleted task completes
without a read. It binds the static Dex connection `asana-task-watcher` and the
Trigger binding `project-task-changes`; `getTask`'s optional branches are
unwired, so a provider rejection fails the Flow.

`main.py` is the Worker entrypoint. It loads the project, runs the Worker, and
runs `new_project_task_trigger_runner`, which records each task event in the
binding's project Trigger inbox before the sync token moves past it and starts
one Flow per Trigger event ID with `new_dex_flow_trigger_target`. The first
start records a baseline and starts no Flow for existing tasks.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer from
`connectors/asana`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/task_changed/flow.py \
  --schema-version 2.0 --json --out ./python/build/task_changed
```

1. In Dex Web Connections, configure an Asana personal access token for
   `asana-task-watcher`. Its `pollInterval` setting is the delay between polls,
   at least 5 seconds.
2. Save the `project-task-changes` binding's watched project. Dex CLI v1.47.3
   does not read the Python binding's `projectPicker` unit yet, so enter the
   binding configuration `{"projectId": "<project gid>"}`, the long number
   after `/project/` in the project's Asana web address, in the binding's
   `triggerBindings` entry of `connections.json`.
3. Start a local Dex server, such as `dexcli dev -dex-port 22240 -web-port 22241 -open=false -flow-rendering-dir python/build`.
4. From `connectors/asana/python`, run `uv sync --locked`, then
   `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run python -m examples.task_changed.main`.
   Restart it after changing the binding; a changed project records a new
   baseline. Under `dexcli dev` the poll cursor stays in process memory, so a
   restart also records a new baseline.

The deterministic tests are `tests/test_task_changed_example.py` and
`tests/test_triggers.py`. With `dexcli dev` running,
`DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest integration/test_task_changed.py`
runs the example's Flow and target against real Dex and a local stand-in for
the Asana Events API: the baseline starts no Flow, each task event starts one
Flow, a comment story starts none, events made while the Trigger process is
stopped arrive after a restart whose first poll answers 429, and an expired
sync token records a new baseline without delivering the gap. Live Asana
authorization and Events API behavior require a user-controlled connection and
remain unverified.
