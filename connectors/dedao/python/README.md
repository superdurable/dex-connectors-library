# Dedao Python connector

`dex-connector-dedao` provides the `getNoteTaskStatus` Query for an existing asynchronous Get Note task. It posts `task_id` to the fixed [Get Note OpenAPI progress endpoint](https://doc.biji.com/docs/WOxgwObNNiyMHWk1dl0cJqSxnEd/) and returns only the task ID, status, and optional note ID. It never returns note content or provider error text.

## Connection

At [Get Note OpenAPI](https://www.biji.com/openapi), create an application and copy its secret `gk_live_` API Key and paired secret `cli_` Client ID into Dex Connections as `apiKey` and `clientID`. Blank credentials prevent requests. Leave `timeout` and `maxResponseBytes` blank for their displayed defaults; they are a Go duration and byte limit. There are no operation configuration fields. Use `data.tasks[0].task_id` from a prior asynchronous note save as the `taskID` input. The provider recommends polling at 10–30 second intervals. Status may be `pending`, `processing`, `success`, or `failed`; `failed` is a returned task status, not a provider rejection.

See `examples/get_note_task_status/` for a runnable Dex Flow and Worker. Run `uv run pytest tests` to replay Go-recorded HTTP scenarios. Live Get Note authorization requires a user-controlled Dex Connection and was not exercised by the recorded fixture.
