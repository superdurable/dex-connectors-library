# Asana connector for Dex TypeScript

Install `@superdurable/connector-asana@0.2.0` alongside `@superdurable/dex` 1.45.4 or newer and `@superdurable/connectors-sdk` 0.12.0. This package implements `listTasks`, `getTask`, `createTask`, `updateTask`, and `addComment`, and, from 0.2.0, the `taskChanged` poll Trigger at parity with Go.

`Client` takes a `Config` and a `CredentialProvider<Credentials>`. Use `newProjectConnection` to load the named Dex Web connection, or `newConnection` to bind a client explicitly. The [approved request example](examples/approved-request-task/flow.ts) is a runnable Flow using every operation and the static `asana-approved-requests` connection name. Start its [Worker](examples/approved-request-task/main.ts) after configuring that connection in Dex Web. The [example guide](examples/approved-request-task/README.md) gives local verification commands.

The personal access token is resolved at each invocation and is sent only in the `Authorization` header. Task and comment POSTs have no Asana idempotency key: they retry a proven unsent request, 429, or 503; an ambiguous outcome selects `uncertain`. A Dex heartbeat checkpoints a returned task or story GID so a resumed attempt reads it back without sending another POST. `updateTask` uses absolute values and may retry; reads retry 408, 429, 5xx, and transport failures. An unwired optional branch fails the Flow.

Task IDs are decimal Asana GIDs. `listTasks` selects one project or section and returns one page of up to 100 tasks. `getTask` includes notes and custom fields. `createTask` accepts a project or workspace, optional section, assignee, date, and text, number, enum, or multi-enum custom fields. `updateTask` completes, reopens, assigns, unassigns, reschedules, clears a due date, sets custom fields, and moves to a section. `addComment` sends plain text to one task.

## taskChanged Trigger

`taskChanged` polls one project's Asana Events API stream, `GET /events?resource=<project gid>&sync=<token>`, on the SDK's durable poll Trigger kit (`@superdurable/connectors-sdk/pollingtrigger`). The [connector README](../README.md#taskchanged-trigger) describes the baseline, payload, expired tokens, rate limits, and delivery guarantees; this package keeps every one of them:

- The first poll sends no sync token; Asana's 412 carries the baseline token, so existing tasks are never delivered.
- Each poll follows `has_more` through at most 10 pages of 100 events and keeps only task events; story, section, and project events are dropped.
- A 412 for a stored token, an undecodable cursor, or a cursor for another project reseeds and logs `poll cursor invalid; reseeding` at WARN. A 429 pauses every binding of the connection for `Retry-After`; other failures back off per binding. None of them moves the cursor.
- The cursor token is `{"projectId":"<gid>","sync":"<token>"}`, and each Trigger event ID is `asana-` plus 32 hex digits of a SHA-256 over the requested sync token and the event's immutable facts, with change values compacted as Go's `json.Compact` does. Both match Go byte for byte, so Go and TypeScript replicas share one binding's poll cursor and Trigger inbox.
- The connection's `pollInterval` (Go nanoseconds in the saved configuration, `Config.pollIntervalMs` in TypeScript) is the delay between polls, at least 5 seconds; `Client` refuses a shorter one. `ClientOptions.logger` receives the poll records.

The payload is `TaskChangedEvent`; `TaskChangeAction` names its five `action` values, and `TaskChangedEventSchema` decodes a stored payload. `newProjectTaskTriggerRunner` is the durable path: it opens the connection, loads each binding's saved project, records each event in the binding's project Trigger inbox, and commits the sync token in the binding's project poll cursor only after that. From [`examples/task-changed/main.ts`](examples/task-changed/main.ts):

```ts
// Each task event is recorded in the binding's project Trigger inbox before the sync token moves past it.
const runner = newProjectTaskTriggerRunner(project, ConnectionName, {
  taskChangedRoutes: [{ bindingName: TaskChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryTaskChange, resolveFlowId, mapToFlowInput) }],
});
```

`newTaskTriggerRunner` takes targets and cursor stores directly, and `newTaskChangedTrigger` builds one binding's `TriggerRunner` with its cursor in memory, so it records a new baseline on every start; use it only for tests and demonstrations. Under `dexcli dev` the project poll cursor also stays in process memory; hosted project storage keeps it. A binding without a saved project, or with an invalid one, throws. A Flow declares the binding with `defineTaskChangedTriggerBinding` and the `projectPicker` unit, bound to `/workspaceId`, `/projectId`, and `/projectName`, as the [task-changed example](examples/task-changed/README.md) does.

## Verification

Run `npm ci` and `npm run check` from this directory. Cross-language provider parity is checked against the Go recordings in [scenarios.json](../../../conformance/connectors/asana/scenarios.json). The real Dex integration tests require `DEX_FLOW_SERVICE_ADDRESS` and use local stand-ins for Asana; `test/trigger.test.ts` checks the fetcher, including Go-computed event IDs and cursor tokens, and `test/integration/task-changed.integration.test.ts` runs the task-changed example's Flow and target against real Dex: a baseline that starts no Flow, one Flow per task event, no Flow for a comment story, delivery across a restart whose first poll answers 429, and an expired sync token that records a new baseline without delivering the gap. Live Asana authorization, API, and Events API behavior remain unverified for this TypeScript package.
