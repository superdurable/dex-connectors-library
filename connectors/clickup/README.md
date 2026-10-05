# ClickUp Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for ClickUp; no live ClickUp Workspace was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module searches, reads, creates, and updates ClickUp tasks, tags and
comments on them, finds Workspace members by email, and receives signed task
webhooks through ClickUp API v2, `https://api.clickup.com/api/v2`:

| Operation | Kind | ClickUp endpoint | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `searchTasks` | Query | `GET /team/{team_id}/task` | async | `searched` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getTask` | Query | `GET /task/{task_id}` | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createTask` | Mutation | `POST /list/{list_id}/task` | sync | `created` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `updateTask` | Mutation | `PUT /task/{task_id}` | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `updateTaskTags` | Mutation | `POST` and `DELETE /task/{task_id}/tag/{tag_name}`, then `GET /task/{task_id}` | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `addComment` | Mutation | `POST /task/{task_id}/comment` | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `findMemberByEmail` | Query | `GET /team` | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

The `taskEvent` Trigger receives ClickUp's task webhook events, verified with
their `X-Signature`.

## Vocabulary

ClickUp's hierarchy is Workspace > Space > Folder > List > task. The API calls a
Workspace a `team`, so `workspaceId` is ClickUp's `team_id`, and a Folder a
`project`. Workspace, Space, Folder, and List IDs are strings of digits; task
IDs are short codes such as `86b2x4k7q`, the part after `/t/` in a task's web
address. User IDs are integers.

The connector passes ClickUp's own values through instead of inventing a
vocabulary:

- A task's **status** is a status name of its List's workflow, such as
  `to do`, `in progress`, or `complete`, with ClickUp's status type (`open`,
  `custom`, `done`, or `closed`). ClickUp has no transition rules: `updateTask`
  sets any status of the List, and a name that is not in the List is rejected.
  Each List can define its own statuses, so a Flow names them as the List does.
- **Priority** is ClickUp's integer: 1 urgent, 2 high, 3 normal, 4 low, and
  zero for none.
- **Dates** are ClickUp's Unix milliseconds, returned as UTC times. Inputs take
  an RFC 3339 time and set ClickUp's `due_date_time`, so the due date keeps its
  time of day.

Results, receipts, failures, and logs never contain the token, the webhook
secret, or ClickUp's error text. A rejection names only the HTTP status and
ClickUp's `ECODE`, such as `ClickUp rejected the request (HTTP 400)
[ITEM_015]`, because the `err` text is free prose. Task assignees and creators
carry a user ID and a display name, never an email address.

## Authentication

A connection holds one ClickUp personal API token, sent as the raw
`Authorization: pk_...` header that ClickUp documents for personal tokens,
without a `Bearer` scheme:

1. Sign in to ClickUp as the user the Flows should act as. Tasks, comments,
   and webhooks are created as that user, and the user's Workspaces, Spaces, and
   Lists limit what the token can reach.
2. Open your avatar > **Settings** > **Apps**
   (<https://app.clickup.com/settings/apps>) and choose **Generate** under
   **API Token**, or **Regenerate** to replace the current token.
3. Choose **Copy** and paste the token into `api_token`. It starts with `pk_`
   and never expires; the connector rejects a value without that prefix before
   any request. Regenerate the token to revoke it, then paste the new one; the
   credential is reread before every call, so no restart is needed.

`webhook_secret` holds the signing secret of the webhook that delivers task
events to this connection; see [taskEvent Trigger](#taskevent-trigger). Leave it
blank when no Flow uses the Trigger.

### ClickUp OAuth is not in this release

ClickUp's OAuth was checked against its documentation and OpenAPI reference,
then deferred:

- ClickUp OAuth has no scopes. The authorization URL takes only `client_id`,
  `redirect_uri`, and `state`, and the token response carries only
  `access_token`. The manifest schema requires at least one OAuth scope, so a
  ClickUp method would have to send an invented scope to ClickUp's consent page.
- The token response documents no `token_type`, `expires_in`, or
  `refresh_token`; access tokens do not expire. A driver would keep a token
  without expiry (`oauthtoken.KeepWhenExpiryMissing`) and could only require
  reauthorization, never refresh. Whether Dex Web accepts a token response
  without `token_type` is unverified.
- ClickUp's OpenAPI lists both JSON and form bodies for `POST /oauth/token`,
  which Dex Web sends form-encoded, but its FAQ says form-encoded data "is not
  fully supported and can result in unexpected consequences".

OAuth would also unlock Studio pickers, because ClickUp sends OAuth tokens as
`Authorization: Bearer`; see [Studio units](#studio-units).

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup. A Trigger application opens it through
the connector's endpoint runner, as
[`examples/escalate-blocked-task/main.go`](examples/escalate-blocked-task/main.go)
does:

```go
func newBlockedStatusEndpointRunner(
	project *projectconfig.LoadedProject, client *dex.Client, flow *escalateblocked.Flow, logger *slog.Logger, connectionOptions []clickup.Option,
) (*clickup.TaskEventEndpointRunner, error) {
	return clickup.NewProjectTaskEventEndpointRunner(project, escalateblocked.ConnectionName, []clickup.ProjectTaskEventTriggerRoute{{
		BindingName: escalateblocked.BlockedStatusTriggerBinding, Target: newBlockedStatusTarget(client, flow, logger),
	}}, connectionOptions...)
}
```

Dex Web or Superverse Studio writes the connection; the application reads it
through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading):

```json
{
  "connectorId": "clickup",
  "connectionName": "clickup-workspace",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/clickup",
  "provider": "clickup",
  "configuration": {"maxResponseBytes": 4194304}
}
```

The stored credential holds `api_token` and, for the Trigger,
`webhook_secret`; both are resolved for every call or webhook request.
`maxResponseBytes` and `webhookMaxBodyBytes` are startup configuration.

## Operations

Each request is bounded by 9 seconds, and `updateTaskTags`, which sends
several, by 25 seconds in total, below the 30-second Execute timeout.
Redirects are never followed, and a `2xx` response that contains the token is
never returned.

| ClickUp answer | Reads, `updateTask`, `updateTaskTags` | `createTask`, `addComment` |
| --- | --- | --- |
| `429` | Retry after `X-RateLimit-Reset`, at most one minute | Retry; ClickUp applied nothing |
| `408`, `5xx`, dropped connection | Retry | Keep the dispatch checkpoint and retry, so the next attempt reads back |
| `404`, or `401` with a Workspace-not-authorized `ECODE` (`OAUTH_023`, `OAUTH_026`, `OAUTH_027`, `OAUTH_029` to `OAUTH_045`) | `notFound` | `notFound` |
| other `401`, `403`, `400`, other `4xx`, `3xx` | `providerRejected` | `providerRejected` |
| unusable or oversized `2xx` | `invalidResponse` | Retry with the checkpoint held |
| invalid input or token | `defect`, no request | `defect`, no request |

ClickUp's rate limit is per token and per minute (100 requests on Free,
Unlimited, and Business plans; 1,000 on Business Plus; 10,000 on Enterprise), so
the retry policy allows six attempts in five minutes with up to a minute
between them.

### searchTasks

`SearchTasksInput.WorkspaceID` is required. `ListIDs`, `FolderIDs`, `SpaceIDs`,
`Statuses`, `AssigneeIDs`, and `Tags` narrow the search, each with at most 50
values that are alternatives. `UpdatedAfter` sends `date_updated_gt` for
polling changes since a checkpoint. `IsClosedIncluded` and
`AreSubtasksIncluded` include closed tasks and subtasks, which ClickUp leaves
out by default. `OrderBy` is `created` (ClickUp's default), `updated`,
`due_date`, or `id`, and `IsReverse` reverses it.

ClickUp returns at most 100 tasks per page with a zero-based `page` number.
`NextPage` is set while ClickUp reports `last_page: false`, or, when the
response has no `last_page`, while a page is full. Pages are numbered, not
cursors, so a task that changes between two page reads can move between pages;
order by `created` and filter with `UpdatedAfter` when polling. Listed tasks
carry no description or custom fields; read them with `getTask`.

### getTask

`GetTaskInput.TaskID` is a task ID, or, with `IsCustomTaskID` and
`WorkspaceID`, a custom task ID such as `DEV-123` (`custom_task_ids=true`).
The `Task` adds the Markdown description (`include_markdown_description=true`)
of at most 65536 bytes (`IsDescriptionTruncated`) and up to 100 custom fields,
each with ClickUp's `id`, `name`, `type`, and its `value` passed through as
JSON: ClickUp's custom field values are typed by field type (a `drop_down`
holds an option ID or index, `users` a list of users), and only set fields
carry one.

### createTask

`CreateTaskInput` names the `ListID` and `Name` and may set
`MarkdownDescription`, `Status`, `Priority`, `DueAt`, `AssigneeIDs`, `Tags`,
`ParentTaskID` (a subtask in the same List), and `CustomFields`, each an ID
and a raw JSON value. ClickUp has no idempotency key, so the Step uses sync
durability and records a Dex heartbeat checkpoint before the request; a later
attempt that finds the checkpoint reads back instead of resending:

| Outcome | Result |
| --- | --- |
| `200` with a task | `created` |
| `429`, or a connection that never opened | checkpoint cleared, Retry, and the create is sent again |
| timeout, dropped connection, `408`, `5xx`, or an unusable `2xx` | checkpoint kept, Retry; the next attempt reads back |
| `404` | `notFound` |
| `400`, `401`, `403`, other `4xx` | `providerRejected` |

An attempt that finds the checkpoint never sends. It reads
`GET /list/{list_id}/task` for tasks created since five minutes before the
dispatch, closed tasks and subtasks included, and reports `created` with
`WasAlreadyApplied` when exactly one has the requested name and parent;
otherwise it selects `uncertain`. Put the application's own request key in the
name, as the example puts the blocked task's ID in brackets, so the read-back
is unambiguous.

### updateTask

`UpdateTaskInput` changes only what it sets: `Name`, `MarkdownDescription` (a
pointer to `""` clears it with ClickUp's documented single space), `Status`,
`Priority`, `DueAt`, and `AddAssigneeIDs` and `RemoveAssigneeIDs`, ClickUp's
`assignees.add` and `assignees.rem`. Every change is an absolute value or a set
membership, so a repeated `PUT` leaves the same task, and every ambiguous
outcome retries. The `Value` is the task ClickUp returns. Clearing the
priority or the due date is not offered.

### updateTaskTags

`UpdateTaskTagsInput` adds and removes up to ten tag names together; a tag in
both lists, a `/`, and the names `.` and `..`, which would change the request
path, are rejected. Each change is its own request, and the operation then
reads the task back for its tags. A rejection after some changes reports
`AppliedChanges`; a retry repeats them all, which changes nothing.

### addComment

`AddCommentInput.Text` is plain text of at most 32768 bytes, sent as
`comment_text`, with `notify_all` from `IsCreatorNotified`. The dispatch rule
matches `createTask`; the read-back is `GET /task/{task_id}/comment`, the 25
newest comments, matched by text, ignoring whitespace differences, and created
since the dispatch.

### findMemberByEmail

`FindMemberByEmailInput` names a `WorkspaceID` and one bare email address. The
operation reads `GET /team`, the Workspaces the token's user belongs to with
their members, and returns the member of that Workspace whose email equals the
address without regard to case: its user ID for `AssigneeIDs`, display name,
email, and role (1 owner, 2 admin, 3 member, 4 guest). `notFound` means no
member has the address, or the token's user is not in the Workspace.

## Avoiding duplicate tasks and comments

`createTask` and `addComment` use sync Execute durability. With async
durability, Dex runs a Step in a local phase of about seven seconds and then
dispatches a backup attempt, so a slow create would be sent twice. The
example's real-Dex tests hold a create, and a comment, for nine seconds and
record one request each; another holds a create while the Worker is replaced,
and the new attempt reads the List instead of resending. Do not override these
Steps to async. Dex accepts the checkpoint when the Worker writes it to its
stream, before the request leaves; a Worker lost in that instant, before Dex
stored the checkpoint, could still send twice, and a crash after the
checkpoint but before the request left reports `uncertain` for a request
ClickUp never received.

`updateTask` and `updateTaskTags` keep the async default. With a fake that held
the `PUT` for nine seconds, Dex dispatched a backup attempt, ClickUp received
the same `PUT` twice, and the task ended with one assignment, as with one
request.

## taskEvent Trigger

ClickUp sends webhook events without a verification handshake, signed with a
secret unique to each webhook. Register one webhook per connection with the
connection's token:

```bash
curl -X POST "https://api.clickup.com/api/v2/team/$CLICKUP_WORKSPACE_ID/webhook" \
  -H "Authorization: $CLICKUP_API_TOKEN" -H "Content-Type: application/json" \
  -d @webhook.json
```

with a `webhook.json` such as:

```json
{
  "endpoint": "https://hooks.example.com/webhooks/clickup",
  "events": ["taskStatusUpdated"],
  "list_id": 901410000001
}
```

`space_id`, `folder_id`, `list_id`, or `task_id` narrows the events to one
location. Copy `webhook.secret` from the response into the connection's
`webhook_secret`; ClickUp returns it only there. `GET /team/{team_id}/webhook`
lists the webhooks and `DELETE /webhook/{webhook_id}` removes one.

The application mounts `Connection.TaskEventWebhookHandler`, or the runner from
`NewProjectTaskEventEndpointRunner`, at that endpoint. For each `POST` the
endpoint:

- answers `413` above `webhookMaxBodyBytes`, `503` while `webhook_secret` is
  blank or no binding runs, and `400` when `X-Signature`, the hex HMAC-SHA256
  of the raw body under the secret, does not match or the body is not a task
  event. It never answers `401`, which suspends a ClickUp webhook at once;
- answers `200` to a valid event that is not a task event, such as
  `listCreated`, without recording it;
- records a task event in every accepting binding's durable inbox before it
  answers `200`, then delivers it in arrival order.

ClickUp retries an event up to five times when the endpoint fails or takes
longer than seven seconds, and counts failures toward suspending the webhook.
The event ID is `{webhook_id}:{event}:{history_item_id}` of the first history
item, following ClickUp's advice to deduplicate on webhook and history item, or
`{webhook_id}:{event}:{task_id}` for `taskDeleted`, which has none. The event
name is part of the ID because ClickUp sends one change as several events, such
as `taskCreated` and `taskStatusUpdated`, with the same history item.
`TaskEvent` carries the history items' IDs, fields, times, user IDs, and, for a
status change, the status before and after; it never carries other changed
values, which can hold task text. ClickUp does not guarantee delivery order, so
compare `OccurredAt`.

## Studio units

`taskEventPicker` lets a binding choose the task events it accepts, from the
list the Trigger decodes; it needs no provider call.

There are no Workspace, Space, or List pickers. ClickUp's hierarchy reads are
fixed-host `GET` requests that return JSON objects, but Studio commands send a
credential only as `Authorization: Bearer <secret>` or in a header other than
`Authorization`, and ClickUp documents personal tokens only as the raw
`Authorization` value. Flows therefore take IDs from their own configuration,
as the example does; each ID's place in a ClickUp web address is described with
the input field.

## Example

[`examples/escalate-blocked-task`](examples/escalate-blocked-task) escalates a
task that moves to the blocked status: the `taskEvent` Trigger starts the Flow,
which reads the task, finds the manager by email, reuses or creates the
escalation task once, tags the blocked task, assigns the manager, and comments
with the link. It uses all seven operations, the Trigger, and the
`taskEventPicker` unit.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, Trigger, persistence, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/clickup/connector.yaml
go run ./cmd/connectorctl generate --check connectors/clickup/connector.yaml
(cd connectors/clickup/ui && npm ci && npm test && npm run build)
```

The deterministic fakes cover every branch above, request bodies and query
parameters, paging, custom task IDs, description truncation, custom field
pass-through, rate limits, redirects, oversized and malformed responses, token
reflection, tokens without the `pk_` prefix, signature checks, event IDs, and
provider message text that never reaches a Result.

No live ClickUp Workspace was used. The following live behavior is
unverified: the Settings > Apps labels in the setup guidance; real response
shapes, including `last_page` on `GET /team/{team_id}/task`, numeric versus
string IDs, and member `email` and `role` on `GET /team`; the status and
`ECODE` ClickUp answers for a missing task, an unknown status, an unknown tag,
and a task in a Workspace the token cannot see (the mapping uses ClickUp's
documented `OAUTH_` codes); whether `assignees.add` of an assigned user and
`DELETE` of an absent tag are accepted; the description-clearing single space;
`date_created_gt` on `GET /list/{list_id}/task`; webhook payload shapes beyond
the documented examples, the hex case of `X-Signature`, and retry timing; and
429 behavior under load.
