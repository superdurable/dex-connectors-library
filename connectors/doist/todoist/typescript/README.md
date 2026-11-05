# Todoist TypeScript connector

Install `@superdurable/connector-doist-todoist@0.1.1` with `@superdurable/connectors-sdk@0.1.1` and a compatible released `@superdurable/dex` package. The connector provides `getTask`, `updateTaskContent`, `listTasks`, `createTask`, and `deleteTask` as typed Dex Steps. It has no Triggers.

The [task CRUD Flow](examples/task-crud/flow/workflow.ts) and [Worker entrypoint](examples/task-crud/main.ts) show the static `ConnectionName` (`todoist`), project connection loading, and all five operations. [Run instructions](examples/task-crud/README.md) cover a disposable task and local Dex server. The integration test uses a provider stand-in; live Todoist authorization and API behavior remain unverified.

Provide a personal API token through Dex Web Connections. Start at [Todoist Developer settings](https://app.todoist.com/app/settings/integrations/developer), open Settings > Integrations > Developer, and copy the API token without a `Bearer` prefix. Keep it in the connection credential. `baseUrl` defaults to Todoist's public API; override it only for a compatible HTTPS gateway or loopback server. `timeout` accepts a positive Go duration such as `15s`; fractional units follow Go's nanosecond precision before rounding up to whole milliseconds. `maxResponseBytes` is a positive byte limit.

`TaskID` is an opaque Todoist ID. Create requires nonblank UTF-8 content. Update replaces only specified content or description; an empty description clears it. `projectId` on creation chooses a project, while an omitted value uses Inbox. Get, update, and create return a bounded `Task` projection. List returns one cursor page. Delete targets the immutable task ID. Provider text is untrusted and must be escaped when rendered.

A selected optional branch fails the Flow unless the application wires it. The Flow example wires only happy paths. Rate limits and safe transport failures retry. Create can return `uncertain` when the provider might have accepted a write without a usable confirmation; inspect that result before starting a new creation. The [Go connector README](../README.md) describes the provider request and recovery contract in detail.
