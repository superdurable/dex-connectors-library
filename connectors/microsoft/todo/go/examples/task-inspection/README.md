# Microsoft To Do task inspection Flows

This runnable Worker registers `MicrosoftTodoListInspection`, `MicrosoftTodoTaskInspection`, and `MicrosoftTodoGetTask` with the static `microsoft-todo` connection. Start the first Flow with `{}` to read list IDs, then the second with `{"listId":"<list ID>"}` to read task IDs. Start the third with `{"listId":"<list ID>","taskId":"<task ID>"}` to inspect one task. Each returns its typed result. Optional branches are unwired, so a rejected provider response fails the Flow.

## Strict FDG 2.0 generation

From `connectors/microsoft/todo/go`, run:

```bash
dexcli visualize ./examples/task-inspection/flow/lists/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-todo-list-fdg
dexcli visualize ./examples/task-inspection/flow/tasks/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-todo-task-fdg
dexcli visualize ./examples/task-inspection/flow/tasks/get_task_workflow.go --schema-version 2.0 --json --out /tmp/microsoft-todo-get-task-fdg
```

The result must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build this connector's release artifact, then run `dexcli dev -open=false -dex-port 9510 -web-port 9511` with `--connector-release-override microsoft-todo=<artifact-directory>`. In Dex Web Connections, configure `microsoft-todo` using the [connection guide](../../../README.md). With the project's `DEX_PROJECT_*` environment set, run from `connectors/microsoft/todo/go`:

```bash
GOWORK=off go run ./examples/task-inspection
```

Start each Flow with a unique ID. Use IDs returned by the list and task Flows. The Worker connects to Dex Server without an inbound listener. The integration test exercises all three Flows against generated mocks and a real Dex Worker and Client. Real Microsoft Graph authorization and retrieval remain unverified.
