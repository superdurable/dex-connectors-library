# Dedao Brain / Get Note connector

`GetNoteTaskStatus` checks an existing asynchronous note-save task. The official [Get Note OpenAPI](https://doc.biji.com/docs/WOxgwObNNiyMHWk1dl0cJqSxnEd/) specifies the `POST /open/api/v1/resource/note/task/progress` read request, API Key and Client ID headers, and `data.status` / `data.note_id` response. The connector returns only task ID, status, and resulting note ID; it does not request note text or attachments.

## Authorization

Sign in to the [Get Note Open Platform](https://www.biji.com/openapi), create an application, and save its `gk_live_` API Key and paired `cli_` Client ID as secret Dex Web Connection fields. Leave timeout and maximum response size blank for the displayed defaults. The official page recommends polling task progress every 10–30 seconds.

## Example and verification

The [runnable Flow](go/examples/get-note-task-status/) starts with a task ID from `data.tasks[0].task_id` of a prior note-save call and completes with a typed `GetNoteTaskStatusOutput`. Its default mock returns a completed task. Live Get Note authorization and status response variation remain unverified without a user-controlled Dex Web Connection.
