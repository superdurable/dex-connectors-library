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
| `NewUpdateTaskStep` | Mutation | `UpdateTaskInput{TaskID, Content?, Description?}` | `updated` | `notFound`, `providerRejected`, `defect` |

The [CRUD Flow](examples/task-crud/flow/workflow.go) defaults to read-only listing.
Its explicit lifecycle mode creates its own task, reads it, updates specified fields,
and reads back the confirmed update.
Confirmed matching updates continue to paginated list verification, deletion,
and a final not-found lookup. This single example covers all five operations.
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
returned `Task`. An unusable or mismatched response triggers a read by task ID:
matching fields confirm `updated`; otherwise the absolute replacement retries.
Todoist's GET returns only active tasks, so a 404 from that read means the task
was completed or deleted after the update was sent. The connector then selects
`notFound` instead of retrying, because no repetition can confirm the fields of
an inactive task. A read that Todoist rejects definitely, such as 401 or 403,
selects `providerRejected` with the read's failure kind, for the same reason.
In both cases repeating the same absolute replacement later stays safe.

The runnable CRUD Flow accepts `updateContent` and `updateDescription` to
exercise these semantics on its disposable task. If neither is specified,
it retains the existing example's default title change. For example, this
changes only the new task's description before verifying and deleting it:

```json
{"mode":"lifecycle","content":"Temporary task","description":"Before","updateDescription":"After"}
```

Use `"updateDescription":""` to exercise clearing.

The former task-content example and Worker are replaced by the single CRUD
and recovery example. Its registry registers only `TodoistTaskCRUDFlow`;
old `TodoistTaskContentFlow` executions are not served by this Worker.

Version `v0.2.0` removes the optional `uncertain` branch from
`updateTaskContent`. Applications that wire that branch must remove the target;
the operation ID and other branch IDs remain stable.

## Execution and recovery

Each attempt makes one HTTP request, except an unusable update response, or one
that mismatches the requested fields, also reads GET `/tasks/{task_id}`, as does
a create response with a task ID but unusable fields. Other calls use GET/POST
`/tasks` for list/create, or GET/POST/DELETE `/tasks/{task_id}` for
read/update/delete. Steps use
async durability, a 30-second Execute timeout, and at most five attempts within
two minutes. Backoff starts at one second and caps at 30 seconds.
An unwired optional branch fails the Flow.

Every request ends before the attempt's Execute deadline, so Dex records the
operation's own result instead of timing out the attempt and running it again.
The attempt time is the 30-second Execute timeout, or less when the Step
context's deadline is earlier; an async Step's first attempt may get only about
seven seconds. A create or update POST must finish within seven twelfths of
that time, and every request, including a read-back, within five sixths; the
rest covers recording the result. These limits also cut a longer `Timeout` or
caller-supplied `http.Client.Timeout`. A request with less than a tenth of the
attempt time left is not sent: the attempt retries, except that a skipped
create read-back selects `uncertain`. A create POST cut off by its limit
therefore selects `uncertain`, and an update cut off by its limit retries.
Credentials are resolved once per attempt, before its first request, and a
read-back reuses them.

The existing get/update pair has the following contract:

| Response | GetTask | UpdateTask |
| --- | --- | --- |
| Valid HTTP 2xx | `found` | `updated` when every specified field matches the returned task |
| HTTP 404 | `notFound` | `notFound` |
| HTTP 409 | `providerRejected` with failure kind `CONFLICT` | `providerRejected` with failure kind `CONFLICT` |
| HTTP 429 | Retry, honoring `Retry-After` | Retry, honoring `Retry-After` |
| HTTP 408, 5xx, transport failure | Retry | Retry |
| Malformed, oversized, or mismatched HTTP 2xx | `invalidResponse` | Read by task ID; see below |
| Interrupted HTTP 2xx body | Retry | Read by task ID, as for a malformed answer |
| Other status, including redirects | `providerRejected` | `providerRejected` |
| Invalid input | `defect`, before dispatch | `defect`, before dispatch |
| Credentials missing from project storage, needing reauthorization, not selected for this scope, rejected by the credential broker, or invalid | `defect`, before dispatch | `defect`, before dispatch |
| Any other credential resolution failure, such as a store read failure or broker timeout | Retry, before dispatch | Retry, before dispatch |

The update's read by task ID is classified like the update's own answer:

| Read-back answer | UpdateTask |
| --- | --- |
| Valid task whose specified fields match | `updated` |
| HTTP 404 (completed or deleted task) | `notFound` |
| HTTP 408, 429, 5xx, transport failure, or an unusable or mismatched 2xx | Retry the absolute replacement |
| Any other status, such as 401, 403, 400, 409, or a redirect | `providerRejected` with the read's failure kind |

The update Mutation's stable key is derived from the Dex Call ID. Replacing
specified fields with the same absolute values is replayable; Todoist provider
key deduplication is not assumed. A replay can overwrite a concurrent edit.
The connector retries safe failures until the Step budget runs out, when the
Flow fails. The example reads the task again after a confirmed update.

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
`X-Request-ID` derived from the Dex Call ID. The [current API v1 reference](https://developer.todoist.com/api/v1/)
shows the header in an example but does not specify deduplication retention,
error caching, or the response to a repeated request. The [older REST v1
reference](https://developer.todoist.com/rest/v1/) says a repeated ID discards a
previously processed POST but does not provide those guarantees for API v1.
Any HTTP 2xx with a valid task selects `created` with the task as Todoist
returned it. The API v1 reference documents no rule that the answer echoes the
request, so the returned content, description, and project ID are the stored
values and are not compared with the input. A 2xx without a valid task ID
selects `uncertain`, because the task may exist and its ID is unknown. Invalid input and
the credential failures above select `defect` before dispatch; any other
credential resolution failure retries. A transport failure with trace
evidence that the request never left the process (a DNS, connect, or TLS
failure before any connection was obtained), HTTP 429, and HTTP 503 retry with
the same identity. Any other transport failure, including a lost response or a
custom transport that reports no connection trace, HTTP 408/500 or another
ambiguous server error, and HTTP 409 select `uncertain`. None of these answers
carries a task ID, and a task cannot be found by request without changing
user-visible task fields. Todoist does not document whether 409 means a request
ID was already processed; its failure kind is `CONFLICT`. Other definite
rejections select `providerRejected`. No exactly-once guarantee is made.

A 2xx with a valid task ID but other missing or malformed fields is different:
the task was accepted and its ID is known. The connector confirms it by GET
`/tasks/{task_id}` in the same attempt, never by resending the creation. A
valid task with that ID selects `created`. Any other read answer, including
404, 403, 429, 5xx, a transport failure, an unusable body, or no time left in
the attempt, selects `uncertain`, because a retry would create again. The
`uncertain` result carries no `Task`; the task ID appears only in its safe
failure message, so treat it as unconfirmed rather than as a readable ID.
Do not reconcile ownership by title alone. Creating another Flow is another
logical creation with another request ID.

`DeleteTaskInput` requires an immutable `TaskID`. It deletes that task **and
its subtasks**, as defined by Todoist. Any HTTP 2xx selects `deleted`; 404 selects
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

Provider tests cover request encoding, credentials and credential resolution
failures, rejected and retryable responses, non-200 success statuses, refused
connections, ambiguous writes, malformed/oversized responses, update read-back
outcomes, create confirmation by task ID, attempt deadlines, validation, and
redirect protection.
Real Dex tests cover Attributes, result routing, retry, a create answer
confirmed by task ID, a credential read that fails once, update replacement
after applied/unapplied 503 responses, an unusable update answer whose read-back
finds no active task or is forbidden, retained Display RPCs,
rejection, unwired failure branches, pagination, create request identity across
rate-limit and 503 retries, and delete retry after an applied write. See the
[CRUD example](examples/task-crud/README.md) and
[CRUD and recovery example](examples/task-crud/README.md) for strict FDG generation,
Worker startup, and opt-in live tests of the same checked-in Flows.
