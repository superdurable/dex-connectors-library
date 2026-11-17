# ClickUp TypeScript connector

`@superdurable/connector-clickup` provides all seven ClickUp task operations to Dex TypeScript Flows: `searchTasks`, `getTask`, `createTask`, `updateTask`, `updateTaskTags`, `addComment`, and `findMemberByEmail`. The `taskEvent` webhook Trigger remains Go only. Operation specific Step factories, model validators, connection contracts, and mock cases are generated from the [manifest](../connector.yaml) and Go model types.

## Connect

Install Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. Create a personal API token at [ClickUp Settings > Apps](https://app.clickup.com/settings/apps) and save it as the private `api_token` in a Dex Web Connection named `clickup-workspace`. Keep it out of Flow input, Attributes, results, and logs. The [runnable example](examples/get-task/) loads that connection at Worker startup. The client resolves the current token before each provider request and sends it as the raw Authorization header.

| Step factory | ClickUp API request | Happy branch |
| --- | --- | --- |
| `newSearchTasksStep` | `GET /team/{workspaceId}/task` | `searched` |
| `newGetTaskStep` | `GET /task/{taskId}` | `found` |
| `newCreateTaskStep` | `POST /list/{listId}/task` | `created` |
| `newUpdateTaskStep` | `PUT /task/{taskId}` | `updated` |
| `newUpdateTaskTagsStep` | tag POST and DELETE, then task GET | `updated` |
| `newAddCommentStep` | `POST /task/{taskId}/comment` | `added` |
| `newFindMemberByEmailStep` | `GET /team` | `found` |

Unwired optional branches fail the Flow. `createTask` and `addComment` send behind the SDK single-dispatch fence, whose Dex heartbeat counts each possible send. After an unknown outcome the next attempt reads the write back, by its saved ID or by name or text, and sends again only while possible sends stay within the Step's send cap (three by default); several matches, or no match at the cap, select uncertain. Only HTTP 400, 401, 403, 404, 411, 413, 414, 415, and 422 select a refusal branch. The other mutations repeat safely using absolute changes or tag membership, and retry every other unknown outcome.

Task writes reject duplicate assignee or custom field IDs, non-JSON custom field values, and invalid status names before contacting ClickUp. Tags must be unique names of 1–100 UTF-8 bytes without surrounding whitespace, slashes, control characters, or dot segments (`.` and `..`). `updateTaskTags` bounds the entire write and read-back sequence to 25 seconds.

## Verify

```sh
npm ci
npm run check
```

Run `npm run test:integration` against a local `dexcli dev` with `DEX_FLOW_SERVICE_ADDRESS` set. The integration test runs the example through a real Dex Worker and Client against a local HTTP fixture. The Go recorded conformance suite checks 89 scenarios covering each operation's branches, retries, transport failure, request shape, idempotency key, unsafe tag paths, input validation, read-back by ID and by name, resends below the send cap, and uncertainty at the cap. Live ClickUp authorization and provider behavior have not been verified.
