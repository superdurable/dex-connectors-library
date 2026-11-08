# Microsoft To Do connector

The Go, Python, and TypeScript packages read the signed-in person's To Do task lists and the tasks in one selected list through Microsoft Graph v1.0. `listTaskLists` returns stable list IDs for `listTasks`. Both queries follow Graph pagination, up to 20 pages and 2,000 items, and reject an untrusted next-page host. Requests use the `Tasks.Read` delegated permission. A transient Graph response or transport failure asks Dex to retry; an inaccessible list selects `notFound` for `listTasks`.

## Connection

In Dex Web **Connections**, create `microsoft-todo` and follow the manifest's Microsoft Entra registration guide. The registration must support organizational accounts and have a Web redirect URI exactly matching the one Dex Web displays. Grant delegated Microsoft Graph `Tasks.Read` and `offline_access`, then enter the Application (client) ID and one-time client secret Value and authorize the account. Dex refreshes access tokens and stores the rotating refresh token. Leave `maxResponseBytes` blank for its displayed default.

All three packages implement both Queries. The [Go example](go/examples/task-inspection/README.md), [Python example](python/examples/task_inspection/README.md), and [TypeScript example](typescript/examples/task-inspection/README.md) are runnable Dex Workers. The Go-recorded [conformance scenarios](../../../conformance/connectors/microsoft/todo/scenarios.json) verify identical provider requests, outputs, and retry decisions across all three languages.

The connection reads only the signed-in person's task lists and tasks. It does not create or modify tasks. Personal Microsoft accounts and national-cloud Graph endpoints are not configured by this release.

## Operations

| Operation | Input | Result |
| --- | --- | --- |
| `listTaskLists` | `{}` | `lists`: array of `{id, displayName}` |
| `listTasks` | `listId`: stable ID from `listTaskLists` | `tasks`: array of `{id, title, status}` |

A non-happy branch is optional in the manifest. In the [runnable example](go/examples/task-inspection/README.md), it is unwired and fails the Flow. Each query returns the whole bounded collection; a larger collection selects `invalidResponse` instead of returning a partial result.

The API contract follows Microsoft's [list lists](https://learn.microsoft.com/en-us/graph/api/todo-list-lists?view=graph-rest-1.0) and [list tasks](https://learn.microsoft.com/en-us/graph/api/todotasklist-list-tasks?view=graph-rest-1.0) documentation. Live Microsoft authorization and reads have not been verified.
