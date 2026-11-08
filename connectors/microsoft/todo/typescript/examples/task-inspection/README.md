# Microsoft To Do TypeScript task inspection

The Worker registers `MicrosoftTodoListsInspectionTypeScript` and `MicrosoftTodoTasksInspectionTypeScript` with the static `microsoft-todo` connection. Start the first Flow with `{}` to obtain list IDs, then start the second with `{"listId":"<list ID>"}`. Each completes with a typed collection. Optional provider branches are unwired and fail the Flow.

## Strict FDG 2.0 generation

Build this package, then run from `connectors/microsoft/todo/typescript`:

```bash
dexcli visualize examples/task-inspection/lists-flow.ts --schema-version 2.0 --json --out /tmp/microsoft-todo-ts-lists-fdg
dexcli visualize examples/task-inspection/tasks-flow.ts --schema-version 2.0 --json --out /tmp/microsoft-todo-ts-tasks-fdg
```

Both JSON files must report `valid: true`. An unpublished local checkout needs `node_modules/@superdurable/connector-microsoft-todo` linked to this package so the analyzer can read its manifest.

## Run with Dex Web

Set the project's `DEX_PROJECT_*` environment and configure `microsoft-todo` as described in the [connection guide](../../../README.md). From this package directory, run:

```bash
npm run example:worker
npm run example:start -- lists
npm run example:start -- tasks <list ID>
```

Run the Worker and start commands in separate terminals. The checked-in integration test starts both Flows against a real Dex Worker and Client with a local Graph transport, including a 503 retry. Live Microsoft authorization and reads remain unverified.
