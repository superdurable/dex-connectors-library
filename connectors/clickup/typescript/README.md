# ClickUp TypeScript connector

`@superdurable/connector-clickup` provides seven ClickUp task operations and the signed `taskEvent` webhook Trigger to Dex TypeScript Flows. Operation specific Step factories, model validators, connection contracts, and mock cases are generated from the [manifest](../connector.yaml) and Go model types. The [task event example](examples/task-event/) mounts the durable Trigger endpoint and starts a real Flow for each status change.

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

Task writes reject duplicate assignee or custom field IDs, non-JSON custom field values, and invalid status names before contacting ClickUp. Tags must be unique names of 1–100 UTF-8 bytes without surrounding whitespace, slashes, control characters, or dot segments (`.` and `..`). `updateTaskTags` bounds the entire write and read-back sequence to 25 seconds.

## Verify

```sh
npm ci
npm run check
```

Run `npm run test:integration` against a local `dexcli dev` with `DEX_FLOW_SERVICE_ADDRESS` set. The integration tests run the operation example and signed Trigger example through a real Dex Worker and Client. The Go recorded conformance suite checks 82 provider operation scenarios. Live ClickUp authorization, provider operations, and webhook delivery have not been verified.
