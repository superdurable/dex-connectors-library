# Microsoft To Do connector

The Go package reads the signed-in person's To Do task lists, tasks, and linked resources through Microsoft Graph v1.0. Python and TypeScript retain their original list queries. Collection queries follow Graph pagination, up to 20 pages and 2,000 items, and reject an untrusted next-page host. Requests use the `Tasks.Read` delegated permission. A transient Graph response or transport failure asks Dex to retry; an inaccessible resource selects `notFound`.

## Connection

In Dex Web **Connections**, create `microsoft-todo` and follow the manifest's Microsoft Entra registration guide. The registration must support organizational accounts and have a Web redirect URI exactly matching the one Dex Web displays. Grant delegated Microsoft Graph `Tasks.Read` and `offline_access`, then enter the Application (client) ID and one-time client secret Value and authorize the account. Dex refreshes access tokens and stores the rotating refresh token. Leave `maxResponseBytes` blank for its displayed default.

All three packages implement the original two Queries. The new Go-only Queries cover one list, one task, and linked resources. The [Go example](go/examples/task-inspection/README.md), [Python example](python/examples/task_inspection/README.md), and [TypeScript example](typescript/examples/task-inspection/README.md) are runnable Dex Workers. The Go-recorded [conformance scenarios](../../../conformance/connectors/microsoft/todo/scenarios.json) verify provider requests, outputs, and retry decisions for each implemented operation.

The connection uses `Tasks.Read`. Graph requires `Tasks.ReadWrite` for list, task, and linked-resource writes, so adding those operations would require a connection-breaking scope change. Personal Microsoft accounts and national-cloud Graph endpoints are not configured by this release.

## Operations

| Operation | Input | Result |
| --- | --- | --- |
| `listTaskLists` | `{}` | `lists`: array of `{id, displayName}` |
| `listTasks` | `listId`: stable ID from `listTaskLists` | `tasks`: array of `{id, title, status}` |
| `getTaskList` | `listId`: stable ID from `listTaskLists` | `{id, displayName}` |
| `getTask` | `listId`, `taskId`: stable IDs | `{id, title, status}` |
| `listLinkedResources` | `listId`, `taskId`: stable IDs | `resources`: array of `{id, displayName, applicationName, externalId, webUrl}` |
| `getLinkedResource` | `listId`, `taskId`, `resourceId`: stable IDs | `{id, displayName, applicationName, externalId, webUrl}` |

A non-happy branch is optional in the manifest. In the [runnable example](go/examples/task-inspection/README.md), it is unwired and fails the Flow. Each query returns the whole bounded collection; a larger collection selects `invalidResponse` instead of returning a partial result.

The API contract follows Microsoft's [To Do overview](https://learn.microsoft.com/en-us/graph/api/resources/todo-overview?view=graph-rest-1.0), [task](https://learn.microsoft.com/en-us/graph/api/todotask-get?view=graph-rest-1.0), and [linked-resource](https://learn.microsoft.com/en-us/graph/api/linkedresource-get?view=graph-rest-1.0) documentation. Live Microsoft authorization and reads have not been verified.
