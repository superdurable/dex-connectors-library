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

Unwired optional branches fail the Flow. `createTask` and `addComment` record a Dex heartbeat before their unkeyed write, then read back after an unconfirmed attempt. A result becomes uncertain when read back cannot identify exactly one earlier write. The other mutations repeat safely using absolute changes or tag membership.

## Verify

```sh
npm ci
npm run check
```

Run `npm run test:integration` against a local `dexcli dev` on ports 21100–21199 with `DEX_FLOW_SERVICE_ADDRESS` set. The integration test runs the example through a real Dex Worker and Client against a local HTTP fixture. The Go recorded conformance suite checks 63 scenarios covering each operation's branches, retries, transport failure, request shape, idempotency key, and uncertain read back. Live ClickUp authorization and provider behavior have not been verified.
