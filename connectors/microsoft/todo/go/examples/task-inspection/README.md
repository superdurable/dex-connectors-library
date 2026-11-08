# Microsoft To Do task inspection Flows

This runnable Worker registers `MicrosoftTodoListInspection` and `MicrosoftTodoTaskInspection` with the static `microsoft-todo` connection. Start the first Flow with `{}` to read list IDs, then start the second with `{"listId":"<list ID>"}`. Both return their typed collection as the Flow result. Optional branches are unwired, so a rejected provider response fails the Flow.

## Strict FDG 2.0 generation

From `connectors/microsoft/todo/go`, run:

```bash
dexcli visualize ./examples/task-inspection/flow/lists/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-todo-list-fdg
dexcli visualize ./examples/task-inspection/flow/tasks/workflow.go --schema-version 2.0 --json --out /tmp/microsoft-todo-task-fdg
```

The result must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build this connector's release artifact, then run `dexcli dev` with `--connector-release-override microsoft-todo=<artifact-directory>`. In Dex Web Connections, configure `microsoft-todo` using the [connection guide](../../../README.md). With the project's `DEX_PROJECT_*` environment set, run from `connectors/microsoft/todo/go`:

```bash
GOWORK=off go run ./examples/task-inspection
```

Start each Flow with a unique ID. Use a list ID returned by your own `MicrosoftTodoListInspection` Flow. The Worker connects to Dex Server without an inbound listener. The integration test exercises both Flows against generated mocks and a real Dex Worker and Client. Real Microsoft Graph authorization and retrieval remain unverified.
