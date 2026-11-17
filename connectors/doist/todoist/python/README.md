# Todoist Python connector

Install `dex-connector-doist-todoist==0.1.1` with a compatible Dex Python SDK.
This package implements `getTask`, `updateTaskContent`, `listTasks`,
`createTask`, and `deleteTask` from the Todoist connector manifest. It uses the
same operation and branch identities as the Go connector. The generated models,
Step factories, mock cases, and project connection bind to the static Todoist
connection declared in Dex Web.

## Runnable example

The [task CRUD example](examples/task_crud/README.md) runs all five operations
through a Dex Flow. It creates and deletes a disposable task. Its Worker reads
the `todoist` connection from Dex project configuration, so set the personal
API token through Dex Web Connections before using a real Todoist account.
Only the credential provider sees the token; Flow inputs and outputs contain
safe task fields.

## Provider outcomes

Reads retry transport failures, HTTP 408, 429, and 5xx. Writes select a
refusal branch only for HTTP 400, 401, 403, 404, 411, 413, 414, 415, or 422.
`createTask` runs behind the SDK single-dispatch Fence with
`max_sends=call.max_sends` (3 by default): a failure known not to have sent the
request, HTTP 429, and HTTP 503 retry, and any other unknown outcome, such as a
lost response, 409, or 5xx, resends the same `X-Request-ID` below the cap and
selects `uncertain` at the cap. A returned task ID is saved in the checkpoint
and never resent; with unusable fields it is read back once within the
attempt, and a later attempt reads it back instead of sending.
`updateTaskContent` reads back a malformed or mismatched success and retries
the safe absolute replacement on any unknown outcome. Delete retries any
unknown outcome; a 404 selects `alreadyAbsent`. Optional branches left unwired
by an application fail its Flow.

`UpdateTaskInput.content=None` preserves the title, while
`UpdateTaskInput.description=""` clears the description. A `ListTasksInput`
limit of zero uses Todoist's default page size. The client resolves credentials
per operation attempt, so a token replacement does not require a Worker restart.

## Tests

From the repository root:

```sh
uv sync --locked --project connectors/doist/todoist/python
uv run --project connectors/doist/todoist/python pytest connectors/doist/todoist/python/tests
make test-connector-conformance CONNECTOR_DIRECTORY=connectors/doist/todoist
```

The integration test requires a running `dexcli dev` address in
`DEX_FLOW_SERVICE_ADDRESS` and uses a fake provider with the real Dex Worker
and Client. Authorization and API calls against a real Todoist account remain
unverified.
