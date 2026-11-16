# Asana task-changed example

Every task added to, changed in, removed from, deleted from, or restored to the watched project, subtasks included, starts one `AsanaTaskChangeTypeScript` Flow through the `taskChanged` poll Trigger. The [Flow](flow/workflow.ts) records the delivered change, reads the task back with `newGetTaskStep` (Asana events name the change, not the task's current state), and completes with both. A deleted task completes without a read. It binds the static Dex connection `asana-task-watcher` and the Trigger binding `project-task-changes`, whose `projectPicker` unit saves the workspace ID, project ID, and project name; `getTask`'s optional branches are unwired, so a provider rejection fails the Flow.

The [Worker entrypoint](main.ts) loads the project, runs the Worker, and runs `newProjectTaskTriggerRunner`, which records each task event in the binding's project Trigger inbox before the sync token moves past it and starts one Flow per Trigger event ID with `newDexFlowTriggerTarget`. The first start records a baseline and starts no Flow for existing tasks.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer. The analyzer resolves `@superdurable/connector-asana` from `node_modules`, so run it where the package is installed, such as an application that depends on it:

```bash
dexcli visualize ./flow/workflow.ts --schema-version 2.0 --json --out ./build/task_changed
```

1. Create an Asana personal access token at <https://app.asana.com/0/my-apps> under **Personal access tokens**, then save it in Dex Web **Connections** for the Asana connector as `asana-task-watcher`. Its `pollInterval` setting is the delay between polls, at least 5 seconds.
2. Pick the watched project for the `project-task-changes` binding in Dex Web. Without the picker lists, enter the project gid, the long number after `/project/` in the project's Asana web address.
3. From the repository root, run `npm ci --prefix connectors/asana/typescript`, start a Dex development server, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and the Dex project environment for the connection, then run `npm run example:task-changed --prefix connectors/asana/typescript`. Restart it after changing the binding; a changed project records a new baseline. Under `dexcli dev` the poll cursor stays in process memory, so a restart also records a new baseline.

The deterministic tests are `test/task_changed_workflow.test.ts` and `test/trigger.test.ts`. With a Dex development server running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration --prefix connectors/asana/typescript` runs the example's Flow and target against real Dex and a local stand-in for the Asana Events API: the baseline starts no Flow, each task event starts one Flow, a comment story starts none, events made while the Trigger process is stopped arrive after a restart whose first poll answers 429, and an expired sync token records a new baseline without delivering the gap. Live Asana authorization and Events API behavior require a user-controlled connection and remain unverified.
