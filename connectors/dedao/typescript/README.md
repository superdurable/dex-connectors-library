# Dedao Get Note task status for Dex TypeScript

`@superdurable/connector-dedao` reads an existing asynchronous note-save task. The `getNoteTaskStatus` Query accepts `{ taskID }`, where `taskID` is `data.tasks[0].task_id` from the earlier save call. It returns only the task ID, `pending` / `processing` / `success` / `failed` status, and a note ID if provided. Note content never enters the Flow result. `found` is the happy branch; `providerRejected`, `invalidResponse`, and `defect` are optional terminal branches. Transport, 429, and 5xx failures return Retry.

## Connection

Create a Dex Web Connection named `dedao`. At the [Dedao Open Platform](https://www.biji.com/openapi), create an application and copy its secret `gk_live_` API Key and paired secret `cli_` Client ID into `apiKey` and `clientID`. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults. The [official API documentation](https://doc.biji.com/docs/WOxgwObNNiyMHWk1dl0cJqSxnEd/) specifies POST `/open/api/v1/resource/note/task/progress` with a JSON `task_id` and recommends polling every 10–30 seconds. This Query performs one read per Step execution; callers schedule any additional polling.

## Runnable Flow

The [task status example](examples/task-status/README.md) includes a Worker and Client and persists the status. Install with `npm install`, run `npm run example:worker`, then `npm run example:start -- 69c3995e99f5a67e` after configuring Dex.

Provider conformance replays 15 Go-recorded scenarios. The integration test runs the same Flow through a real Dex Worker against a local provider fixture. Real Dedao authorization and note task lookup remain unverified without a user-owned connection.
