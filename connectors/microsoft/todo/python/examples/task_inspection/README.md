# Microsoft To Do Python task inspection

The Worker registers `MicrosoftTodoListsInspectionPython` and `MicrosoftTodoTasksInspectionPython` with the static `microsoft-todo` connection. Start the first Flow with `{}` to obtain list IDs, then start the second with `{"listId":"<list ID>"}`. Each completes with a typed collection. Optional provider branches are unwired and fail the Flow.

## Strict FDG 2.0 generation

From `connectors/microsoft/todo/python`, run:

```bash
PYTHONPATH=src dexcli visualize examples/task_inspection/lists_flow.py --python .venv/bin/python --schema-version 2.0 --json --out /tmp/microsoft-todo-python-lists-fdg
PYTHONPATH=src dexcli visualize examples/task_inspection/tasks_flow.py --python .venv/bin/python --schema-version 2.0 --json --out /tmp/microsoft-todo-python-tasks-fdg
```

Both JSON files must report `valid: true`.

## Run with Dex Web

Set the project's `DEX_PROJECT_*` environment and configure `microsoft-todo` as described in the [connection guide](../../../README.md). From this package directory, run:

```bash
uv run python -m examples.task_inspection.worker
```

Use unique Flow IDs. The Worker has no inbound listener. The checked-in integration test starts both Flows against a real Dex Worker and Client with a local Graph transport, including a 503 retry. Live Microsoft authorization and reads remain unverified.
