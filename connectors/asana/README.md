# Asana Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Asana; no live Asana account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module lists, reads, creates, and updates Asana tasks and comments on them
through the Asana REST API, `https://app.asana.com/api/1.0`:

| Operation | Kind | Asana endpoint | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `listTasks` | Query | `GET /tasks?project=` or `?section=` | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getTask` | Query | `GET /tasks/{task_gid}` | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createTask` | Mutation | `POST /tasks` | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `updateTask` | Mutation | `POST /sections/{section_gid}/addTask`, then `PUT /tasks/{task_gid}` | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `addComment` | Mutation | `POST /tasks/{task_gid}/stories` | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Asana's only task status is `completed`; section membership carries the
workflow state, so moving a task between sections is its status change.
`updateTask` therefore treats a section move as a first-class change, and every
`Task` carries its project and section memberships.

IDs are Asana gids, decimal strings such as `1204567890123456`, the long numbers
in an Asana web address. Results, receipts, failures, and logs never contain the
token or Asana's error message text: a rejection names only the HTTP status,
such as `Asana rejected the task with HTTP 400`, because Asana's messages are
free text that can repeat task content. Related users carry a gid and a display
name, never an email address.

No Trigger ships in this release. Asana webhooks start with an `X-Hook-Secret`
handshake: Asana sends a test `POST` and requires the receiver to echo the
header back. `sdkgo/webhooktrigger` has no handshake hook yet, and Asana also
refuses `localhost` webhook targets. Until both are solved, poll with
`listTasks` and `ModifiedSince`.

## Authentication

A connection holds one Asana personal access token, sent as
`Authorization: Bearer <token>`:

1. Sign in to Asana as the user the Flows should act as. Tasks, assignments,
   and comments are created as that user in every workspace the user belongs
   to; a dedicated Asana user narrows what a leaked token can reach.
2. Open the developer console at <https://app.asana.com/0/my-apps>, find
   **Personal access tokens**, choose **Create new token**, name it, and accept
   the API terms.
3. Paste the token, shown once, into the connection's secret field. Asana
   documents the format as opaque, so the connector checks only that it is a
   safe header value. The token does not expire; delete it in the same console
   to revoke it, then paste a replacement.

The credential is reread before every call, so a replacement token takes effect
without a restart. A 401 is never resent, because a personal access token
cannot be refreshed.

### Asana OAuth is not in this release

Asana OAuth was verified against Asana's documentation and live OAuth metadata,
then deferred, because neither half of a two-method connection completes in
Dex Web `cli-v1.1.0`:

- Asana's token response (`POST https://app.asana.com/-/oauth_token`,
  form-encoded, `client_secret_post` or `client_secret_basic`, `token_type`
  `bearer`, `expires_in` 3600) carries no `scope` string. Dex Web checks granted
  scopes only in that string and rejects the callback with
  `CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`, and a manifest cannot declare zero
  scopes. Asana reports scopes only through its token introspection endpoint.
- Asana requires an `https` redirect URL for web apps, so a local
  `http://127.0.0.1` Redirect URI is likely refused.
- Dex Web `cli-v1.1.0`'s Connections form adds `credentials.auth_method` when it
  saves an API-key method of a multi-method manifest, and the server rejects
  that field, so the personal access token could not be saved either.
  superdurable/dex#570 fixes this after `cli-v1.1.0`.

Once Dex Web can verify scopes without a `scope` string, OAuth can be added as a
second method with `sdkgo/oauthtoken`: refresh-token grant, form body,
`ClientSecretPost`, the prior refresh token kept when Asana omits one, and
`invalid_grant` treated as reauthorization.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/approved-request-task/main.go`](examples/approved-request-task/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := asana.NewProjectConnection(project, approvedrequesttask.ConnectionName)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `access_token`; the connector resolves it
for every call. `endpoint` and `maxResponseBytes` are startup configuration.

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout. Redirects are never followed.
A response that contains the token is never returned. 408, 429 (after
`Retry-After`, in seconds), 5xx, and transport failures retry reads and
`updateTask`; 404 selects `notFound` where the operation declares it; 400, 402
(a feature above the workspace's plan), 403, and other 4xx select
`providerRejected`. Asana documents no request ID header, so receipts carry the
Call ID and object gid only.

### listTasks

Set exactly one of `ProjectID` and `SectionID`. `IsIncompleteOnly` sends
`completed_since=now`; `CompletedSince` lists incomplete tasks plus tasks
completed since that time; `ModifiedSince` lists only tasks Asana counts as
changed since then, such as assigned, renamed, completed, commented, or added
to a project. `PageSize` is 1 to 100, and zero requests 50. Continue with the
returned `NextOffset`; Asana expires offsets after some time and does not
document the status it then returns, so an expired one selects
`providerRejected` for a 4xx and retries for a 5xx. A listed task carries its memberships but no
notes or custom field values; read them with `getTask`.

Asana's workspace task search (`GET /workspaces/{gid}/tasks/search`) is not
offered: Asana answers it with 402 for non-premium workspaces, indexes changes
10 to 60 seconds late, and has no stable paging. Project and section listing has
none of those limits.

### getTask

`GetTaskInput.TaskID` is a gid. The `Task` adds the workspace, plain-text notes
of at most 65536 characters (`IsNotesTruncated`), and up to 50 custom field
values: Asana's `DisplayValue` for every type, plus the typed value of text,
number, enum, and multi-enum fields.

### createTask

`CreateTaskInput` names exactly one of `ProjectID` and `WorkspaceID`. With a
`SectionID` the task is created in that section of the project through
Asana's create-only `memberships`; otherwise in the project's default section.
`AssigneeID` is `me`, a user gid, or a workspace member's email address.
`DueOn` is `YYYY-MM-DD`. `CustomFields` sets up to 20 values by custom field
gid: `Text`, `Number`, `EnumOptionID` (the option's gid, not its name), or
`MultiEnumOptionIDs`; date and people fields are not supported.

Asana has no idempotency key, so the connector retries only when Asana cannot
have created anything:

| Outcome | Result |
| --- | --- |
| 201 with a task gid | `created` |
| 400, 401, 402, 403, 404, or another 4xx except 408 and 429 | `providerRejected` |
| 429 | Retry, after `Retry-After` |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 408, 3xx, or 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, unusable, or reflects the token | `uncertain` |

On `providerRejected` and `uncertain`, the Value echoes the requested name,
project, section, and workspace without a task gid. Asana's `external.gid`
lookup was not used as a duplicate guard: it needs OAuth, and Asana does not
document that it is unique.

### updateTask

Every change is an absolute value: `IsCompleted` true or false, `AssigneeID`
(a pointer to `""` unassigns), `DueOn` (a pointer to `""` clears),
`CustomFields`, and `SectionID`. The operation moves the task to `SectionID`
first, which removes it from the project's other sections and places it at the
top of the section, then sends the field changes with `PUT /tasks/{task_gid}`,
whose response is the `Task` read back. A move without field changes reads the
task back with `GET`. `IsMovedToSection` reports a confirmed move, also when a
later field change was rejected. Asana does not document what `addTask` does
for a section of a project the task is not in.

Because a repeated move or `PUT` leaves the same task, every ambiguous outcome,
including a timeout after dispatch or a 5xx, retries instead of selecting
`uncertain`. A retry or backup attempt can move the task back to the top of the
section if someone reordered it in between.

### addComment

`AddCommentInput.Text` is plain text of at most 65536 characters, sent as
`text`, never `html_text`, so it is never read as markup. The comment is
authored by the token's user. The branch table matches `createTask`, except that
a 404 selects `notFound`. A read-before-write over the task's stories would not
make a comment safe: two concurrent attempts both read before either writes,
and it would also suppress a deliberately repeated comment.

## Avoiding duplicate tasks and comments

`createTask` and `addComment` use sync Execute durability although a request is
usually fast. With async durability, Dex runs a Step in a local phase of about
seven seconds and then dispatches a backup attempt, so a slow create would be
sent twice. With sync durability, a real Dex run against a fake Asana that held
the create, or the comment, for nine seconds recorded one request;
`TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex` and
`TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex` guard this. Do not
override these Steps to async.

`updateTask` keeps the async default. With a fake that held the `PUT` for nine
seconds, Dex dispatched a backup attempt, Asana received the same move and the
same `PUT` twice, and the task ended assigned, scheduled, and in the section
exactly as with one request;
`TestSlowUpdateBackupAttemptLeavesTheSameTaskWithRealDex` guards this.

The connector never resends a dispatched create or comment, but Dex runs a Step
at least once. If the Worker is lost after Asana accepts a create and before Dex
commits the Step result, Dex runs the Step again and Asana creates a second
task. No connector can close that window without a provider idempotency key. An
application handles `uncertain` without creating again automatically; the
[`approved-request-task`](examples/approved-request-task) example parks the
create for an operator, who confirms the task found in the project or approves
a new create.

## Studio pickers

Both units read through declared read-only Studio commands on the fixed host
`https://app.asana.com`, with the token as a bearer credential that the bundle
never receives:

| Unit | Outputs | Commands |
| --- | --- | --- |
| `workspacePicker` | `workspaceId`, `workspaceName` | `listWorkspaces`: `GET /workspaces` |
| `projectPicker` | `workspaceId`, `projectId`, `projectName`, `sectionId`, `sectionName` | `listWorkspaces`, then `listProjects`: `GET /projects?workspace=&archived=false`, then `listSections`: `GET /projects/{projectId}/sections` |

Each command asks for 100 entries and only `name`, and the bundle follows
Asana's `next_page.offset` through at most 20 pages. Asana answers all three
with a JSON object, as Dex Web requires. When a list cannot load, each unit
offers manual entry of the gid. Use `workspacePicker` on a `createTask` Step
that creates tasks in a workspace without a project.

## Example

[`examples/approved-request-task`](examples/approved-request-task) turns an
approved request into a task: it pages through the project's incomplete tasks
for one that already carries the request ID, creates the task once, assigns it
and moves it to the picked section, and comments with the approval. It uses all
five operations and composes the `projectPicker` unit.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, RPC, persistence, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/asana/connector.yaml
go run ./cmd/connectorctl generate --check connectors/asana/connector.yaml
(cd connectors/asana/ui && npm ci && npm test && npm run build)
```

The deterministic fakes cover every branch above, request bodies and query
parameters, offset paging, notes truncation, custom field encoding, rate limits
with `Retry-After`, redirects, oversized and malformed responses, token
reflection, header-unsafe tokens, and provider message text that never reaches a
Result.

No live Asana account was used. The following live behavior is unverified:
the developer console labels in the setup guidance; real response shapes of
every endpoint and the `opt_fields` this connector requests; `GET /tasks` with
`section` alone, which Asana's FAQ allows but the reference's parameter note
omits; `completed_since=now` and `modified_since` on `GET /tasks`; creating a
task in a section through `memberships`, which the reference marks both
create-only and read-only; `addTask` for a section of another project; the
status of an expired offset; 429 and 5xx behavior under load; and the three
Studio commands through Dex Web.
