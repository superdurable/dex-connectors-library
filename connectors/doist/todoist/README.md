# Todoist Connector

List, read, create, update, and delete tasks through typed Dex Steps using the
[Todoist API v1](https://developer.todoist.com/api/v1/).
The module is `github.com/superdurable/dex-connectors-library/connectors/doist/todoist`;
the connector ID and provider are `todoist`.

| Factory | Kind | Input | Required branch | Optional branches |
| --- | --- | --- | --- | --- |
| `NewListTasksStep` | Query | `ListTasksInput` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `NewCreateTaskStep` | Mutation | `CreateTaskInput` | `created` | `providerRejected`, `uncertain`, `defect` |
| `NewDeleteTaskStep` | Mutation | `DeleteTaskInput` | `deleted` | `alreadyAbsent`, `providerRejected`, `defect` |
| `NewGetTaskStep` | Query | `GetTaskInput{TaskID}` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewUpdateTaskStep` | Mutation | `UpdateTaskInput{TaskID, Content?, Description?}` | `updated` | `notFound`, `providerRejected`, `uncertain`, `defect` |

The [CRUD Flow](examples/task-crud/flow/workflow.go) defaults to read-only listing.
Its explicit lifecycle mode creates its own task, reads it, updates specified fields,
reads back confirmed and uncertain outcomes, and preserves uncertainty in its result.
Confirmed matching updates continue to paginated list verification, deletion,
and a final not-found lookup. This single example covers all five operations
and uncertain-update reconciliation.
The application opens its declared connection with `NewProjectConnection` after
`projectconfig.LoadFromEnvironment`. Tests can construct `New` and
`NewConnection`, with `WithHTTPClient` for a custom transport.

`TaskID` is an opaque Todoist ID, not a URL. `Content` is a complete non-blank UTF-8 title when creating a task or explicitly replacing its title. The encoded update must fit Todoist's 1 MiB request limit. Get, create, and update
return a `Task` projection with `ID`, `ProjectID`, `Content`,
`Description`, and `IsCompleted` (the provider's `checked` field). Additional
provider fields are ignored. Provider text is untrusted; escape it when rendering.
Completed tasks are outside the get-active-task operation's scope.

## Partial updates

`UpdateTaskInput` requires `TaskID` and at least one of `Content` or
`Description`, both `*string`. Nil (or an omitted/null JSON property) leaves
that field unchanged; a pointer to an empty string clears `Description`.
Specified `Content` must remain non-blank. `ProjectID` is supported on creation
only: Todoist uses a separate move endpoint, outside this connector's scope.
The connector sends only specified fields and verifies those fields in the
returned `Task`. It makes no preflight read or additional HTTP request.

The runnable CRUD Flow accepts `updateContent` and `updateDescription` to
exercise these semantics on its disposable task. If neither is specified,
it retains the existing example's default title change. For example, this
changes only the new task's description before verifying and deleting it:

```json
{"mode":"lifecycle","content":"Temporary task","description":"Before","updateDescription":"After"}
```

Use `"updateDescription":""` to exercise clearing. The same Flow handles
uncertain updates by reading back without resending or deleting the task.

The former task-content example and Worker are replaced by the single CRUD
and recovery example. Its registry registers only `TodoistTaskCRUDFlow`;
old `TodoistTaskContentFlow` executions are not served by this Worker.

This unpublished release changes the Go API from `UpdateTaskContent` to
`UpdateTask`, including its factory, input, result, and branch symbols; existing
Go callers must migrate and take the address of specified content values.
The stable manifest operation ID remains `updateTaskContent` to preserve Dex
operation identities. Flow/Step/Attribute identities and JSON string values
remain compatible; the first release version remains `v0.1.0`.

## Execution and recovery

Each attempt makes one HTTP request: GET/POST `/tasks` for list/create, or
GET/POST/DELETE `/tasks/{task_id}` for read/update/delete. Steps use
async durability, a 30-second Execute timeout, and at most five attempts within
two minutes. Backoff starts at one second and caps at 30 seconds.
An unwired optional branch fails the Flow.

The existing get/update pair has the following contract:

| Response | GetTask | UpdateTask |
| --- | --- | --- |
| Valid HTTP 200 | `found` | `updated` when every specified field matches the returned task |
| HTTP 404 | `notFound` | `notFound` |
| HTTP 429 | Retry, honoring `Retry-After` | Retry, honoring `Retry-After` |
| HTTP 408, 5xx, transport failure | Retry | `uncertain` |
| Malformed, mismatched, or oversized HTTP 200 | `invalidResponse` | `uncertain` |
| Interrupted HTTP 200 body | Retry | `uncertain` |
| Other status, including redirects | `providerRejected` | `providerRejected` |
| Invalid input or missing/invalid credentials | `defect`, before dispatch | `defect`, before dispatch |

The update Mutation's stable key is derived from the Dex Call ID. Todoist provider
key deduplication is not assumed. Replacing specified fields with the same absolute values
is replayable, but a replay can overwrite a concurrent edit. Transport loss and
server errors therefore select `uncertain` rather than requesting a blind retry.
A worker crash around dispatch can still cause replay; this is not exactly-once
provider execution. Read back after `uncertain` and let the application decide
how to reconcile. A matching read does not prove which actor made the change.
The example preserves the `uncertain` status even when read-back matches.

## List, create, and delete

`ListTasksInput` supports an optional `ProjectID`, `Cursor`, and `Limit`.
It reads active tasks across projects when the project is blank. A zero limit
uses the provider default of 50; explicit limits are 1 through 200.
`TaskPage.Tasks` contains one page, including an empty page, and `NextCursor`
is empty at the end. Keep the same project and limit when following the cursor
in another Step execution. The connector does not fetch all pages internally
or promise snapshot consistency while tasks change. List retries and invalid
response handling match the get Query; a rejected project/filter selects
`providerRejected`.

`CreateTaskInput` requires `Content`, with optional `Description` and
`ProjectID`. A blank project creates in Inbox. It sends a stable UUID-shaped
`X-Request-ID` derived from the Dex Call ID, consistent with
[Todoist's official SDK request-ID support](https://doist.github.io/todoist-api-python/api/).
HTTP 200 must contain a valid matching task before selecting `created`.
HTTP 429 retries with the same identity. Transport loss, 408/5xx, malformed or
oversized success data, or a 409 duplicate/conflict response selects `uncertain`;
the connector does not invent a task ID or retry creation with a new identity.
Other errors select `providerRejected`. Provider deduplication retention and
successful response replay are not assumed; no exactly-once guarantee is made.
Do not reconcile ownership by title alone. Creating another Flow is another
logical creation with another request ID.

`DeleteTaskInput` requires an immutable `TaskID`. It deletes that task **and
its subtasks**, as defined by Todoist. HTTP 200/204 selects `deleted`; 404 selects
`alreadyAbsent`, meaning missing or inaccessible, possibly after an earlier
successful attempt. It does not prove this caller performed the original
delete. Transport loss, 408/5xx, and 429 safely retry the same task ID. Other
errors select `providerRejected`. `DeleteTaskOutcome.TaskID` identifies the
requested target. Both deletion branches should be wired when an application
wants confirmed absence to complete its process.

Only the initial basic task fields and project filter are supported. Advanced
scheduling, labels, task completion/reopening, and additional list filters are
outside this first release.

## Connection setup

In Dex Web's connector connection form, follow the guide to
[Todoist Settings > Integrations > Developer](https://app.todoist.com/app/settings/integrations/developer).
Copy the personal API token into `api_token` without a `Bearer` prefix. It is
secret and grants access to that Todoist account. Blank credentials prevent
requests. Replace/revoke it from the same provider page, then save the replacement
connection credential. Credentials are resolved on every call.

| Non-secret setting | Default | Override purpose |
| --- | --- | --- |
| `baseUrl` | `https://api.todoist.com/api/v1` | A compatible HTTPS gateway or loopback test server |
| `timeout` | `15s` | Positive Go duration for one HTTP request |
| `maxResponseBytes` | `1048576` | Positive byte limit for unusually large task responses |

Leave these settings blank to use their manifest defaults. There are no operation
or Trigger configuration UI units. Task ID and replacement content belong to the
Flow input, not the connection configuration.

The token travels only in the Authorization header. Redirects are disabled.
Failure messages exclude raw provider bodies and transport errors. Responses
reflecting the token are rejected before returning durable values.

## Verification

From this module:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1
```

Provider tests cover request encoding, credentials, rejected and retryable
responses, ambiguous writes, malformed/oversized responses, validation, and
redirect protection. Real Dex tests cover Attributes, result routing, retry,
read-back after applied/unapplied uncertainty, retained Display RPCs, rejection,
and unwired failure branches, plus pagination, create request identity across
rate-limit retry, and delete retry after an applied write. See the
[CRUD example](examples/task-crud/README.md) and
[CRUD and recovery example](examples/task-crud/README.md) for strict FDG generation,
Worker startup, and opt-in live tests of the same checked-in Flows.
