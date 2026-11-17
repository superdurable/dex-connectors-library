# Jira Cloud Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/atlassian/jira/go@v0.25.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/atlassian/jira`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Jira; no live Atlassian site was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module searches, reads, creates, transitions, and comments on Jira Cloud
issues through Atlassian's OAuth 2.0 (3LO) gateway,
`https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3`:

The [Python package](python/README.md) implements the same five operations at
`v0.2.1`, with generated OAuth refresh, Go-recorded conformance, and a
[runnable Dex Flow](python/examples/issue_lookup/README.md), and from `v0.2.0`
the `issueChanged` Trigger, with a
[runnable issue-changed Flow](python/examples/issue_changed/README.md). Its
local integration tests use a Jira stand-in; live Atlassian authorization and
API behavior remain unverified.

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `searchIssues` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getIssue` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createIssue` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `transitionIssue` | Mutation | async | `transitioned` | `notFound`, `transitionUnavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `addComment` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Status, issue type, and priority names are the site's own values and are never
mapped to another vocabulary; `IssueStatus.CategoryKey` (`new`,
`indeterminate`, or `done`) is the portable signal. Results, receipts,
failures, and logs never contain Jira's `errorMessages` or `errors` text or any
token, and the standard issue fields carry account IDs and display names but
never email addresses. `AdditionalFields` are Jira's raw JSON for the field IDs
an application requests, so request only fields the Flow may store. A rejection
names only the HTTP status and the
field IDs Jira listed, such as `Jira rejected the issue with HTTP 400 (fields:
summary)`, and the same IDs appear in the Value's `RejectedFieldIDs`.

It also exposes one Go Trigger:

- `issueChanged` delivers each issue created or updated in one project, or in
  every project the account can browse, read by polling enhanced JQL search on
  the `updated` time. See [issueChanged Trigger](#issuechanged-trigger). The
  Python and TypeScript packages implement it from 0.2.0, sharing the Go cursor
  documents byte for byte.

| Capability | Go | Python | TypeScript |
| --- | --- | --- | --- |
| `searchIssues`, `getIssue`, `createIssue`, `transitionIssue`, `addComment` | yes | yes | yes |
| `issueChanged` Trigger | yes | yes, from 0.2.0 | yes, from 0.2.0 |

## TypeScript package

The `@superdurable/connector-atlassian-jira` package at `typescript/` is version
`0.2.1` and implements all five operations in this manifest and the
`issueChanged` Trigger, with a
[runnable issue-changed example](typescript/examples/issue-changed/README.md). Its generated
OAuth driver and project credential source refresh rotating Atlassian tokens;
`createIssue` and `addComment` retain Dex single-dispatch and Jira property
read-back behavior. See the [package README](typescript/README.md) and
[runnable issue read example](typescript/examples/get-issue/README.md).

Go-recorded conformance scenarios replay provider requests and branches in
TypeScript. A real Dex integration test uses a local Jira stand-in. Live
Atlassian authorization, site selection, and provider API behavior remain
unverified.

## Atlassian setup

Create one OAuth 2.0 (3LO) app at
<https://developer.atlassian.com/console/myapps/>:

1. **Create > OAuth 2.0 integration**, name the app, and accept the terms.
2. **Permissions > Jira API > Add**, then **Configure** and add the classic
   scopes `read:jira-work` and `write:jira-work`. The connection also requests
   `offline_access`, which makes Atlassian issue a rotating refresh token.
3. **Authorization > OAuth 2.0 (3LO) > Configure**: paste the Redirect URI
   Dex Web shows into **Callback URL** and save.
4. **Settings**: copy **Client ID** and **Secret** into the Dex Web form.
5. Choose **Connect**, pick the Jira site on the consent screen, and accept.
   The app is private to its owner until **Distribution > Enable sharing** is
   on.

Dex requests `audience=api.atlassian.com` and `prompt=consent` and does not
use PKCE, which Atlassian's 3LO flow does not document. The access token lasts
one hour. `CredentialRefreshDriver` refreshes it through
`sdkgo/oauthtoken` with a JSON body, the client presented by
`client_secret_post`, and a response without `token_type` accepted. Atlassian
disables every refresh token when it issues the next one, so the driver always
stores the replacement. Atlassian answers an invalid or expired refresh token
with HTTP 403 and `invalid_grant`, and the driver then reports reauthorization;
a refresh token unused for 90 days expires. A refresh that loses
`read:jira-work` or `write:jira-work` also requires reauthorization.

### Choosing the site

Every operation works on one site, named by the non-secret `cloudId`
configuration (a UUID). The manifest renders the field with the `sitePicker`
unit, which lists the authorization's sites through the read-only
`listAccessibleSites` command
(`GET https://api.atlassian.com/oauth/token/accessible-resources`).

When `cloudId` is blank, the connector calls the same endpoint before its first
operation and uses the only Jira site the grant covers. It selects `defect`
when the grant covers no Jira site or several. The resolved site is cached until
the application restarts.

Two Dex Web `cli-v1.1.0` gaps affect the picker, so the unit always offers
manual entry of the `cloudId` found at
`https://<your-site>.atlassian.net/_edge/tenant_info`:

- Dex Web renders a connection field's `studioUnit` as a plain text field, so
  the connection form shows `cloudId` as text.
- Dex Web accepts only a JSON object from a Studio command, and Atlassian
  answers `accessible-resources` with a top-level array, so the command fails
  with `CONNECTOR_PROVIDER_COMMAND_FAILED`.

### Choosing a project

The `projectPicker` unit lists projects through the read-only `listProjects`
command (`GET https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3/project/search`).
The host stays fixed; only the `cloudId` path segment is a declared parameter,
which Dex Web checks against `^[A-Za-z0-9._~-]+$` and path-escapes. On a
`createIssue` Step it asks Jira for projects the account can create issues in
(`action=create`); on every other Step and on an `issueChanged` Trigger
binding it lists projects the account can browse (`action=browse`). It stores
`projectId`, `projectKey`, and `projectName`. On a Trigger binding a blank key
watches every browsable project.

Dex Web `cli-v1.1.0` sends no connection configuration to a unit, so the unit
cannot read the connection's `cloudId`. It asks for a site `cloudId` used only
for listing, and it always accepts a typed project key. Studio commands send
the stored access token, which Dex Web does not refresh; the Worker's next Jira
call refreshes and persists it, and otherwise **Reconnect** issues a new one.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/triage-issue/main.go`](go/examples/triage-issue/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := jira.NewProjectConnection(project, triageissue.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../../sdkgo/projectconfig/README.md). Credentials
stay in project storage, are read for every provider call, and are refreshed
first when the access token is missing, has no recorded expiry, or expires
within five minutes; `cloudId`, `endpoint`, and `maxResponseBytes` are startup
configuration.

## issueChanged Trigger

`issueChanged` polls Jira's enhanced search,
[`POST /rest/api/3/search/jql`](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/#api-rest-api-3-search-jql-post),
and delivers one `jira.IssueChangedEvent` per new issue version. It is built on
the Connector SDK's durable poll Trigger kit
([`sdkgo/pollingtrigger`](../../../sdkgo/README.md#polling-triggers), sdkgo
v0.38.0) and needs only the `read:jira-work` scope the connection already
grants.

- **Query.** Each poll sends `project = <id> AND updated >= -<N>m ORDER BY
  updated ASC`, with 100 issues per page and the standard issue fields. A blank
  project drops the project clause. `N` is a relative bound in minutes, so the
  query does not depend on the account's time zone, which Jira applies to
  absolute JQL dates. It covers the cursor's latest `updated` time minus a
  two-minute lookback, plus one minute for JQL's minute precision.
- **Cursor.** The cursor keeps the latest `updated` instant read and a tie set
  of `issueID@updatedMillis` versions within the lookback (at most 1,000). A
  re-read issue whose version is in the tie set is skipped. An update that
  Jira's search index shows late, but within the lookback, is still delivered.
  An index delay longer than two minutes can miss an update. Times come from
  Jira's clock, read from each search response's `Date` header, so a skewed
  host clock does not move the window.
- **Baseline.** The first poll of a binding records Jira's current time, or a
  later `updated` time Jira reports, and marks the issues updated within the
  lookback as seen. Existing issues are never delivered.
- **Pages.** Pages continue with `nextPageToken`, repeating the same JQL as Jira
  requires; the kit reads up to 10 pages per poll. Jira does not document that
  a token survives an issue moving in the `updated` order, so once a page moves
  the committed position more than a minute past where the search began, the
  next page is a fresh search from that position; it re-reads an issue an
  update slid back across the page boundary, and the tie set skips the overlap.
  A token is followed only through a burst of issues updated within about
  three minutes. A sequence older than the lookback restarts from the committed
  position, because the relative bound moves with Jira's clock. A page token
  Jira rejects with 400, for example one that expired, also restarts the
  search. None of these loses a change.
- **Change kind.** Search reports no change type, so `ChangeKind` is derived.
  `created` means `updated` is within one second of `created`. Every other
  change is `updated`. Several edits between two polls arrive as one change
  with the latest state. Deleted issues and issues moved out of the project are
  not reported.
- **Reset.** A cursor that cannot be decoded, or that belongs to another site or
  project, is replaced by a new baseline. The kit logs a WARN `poll cursor
  invalid; reseeding` record. Changes between the last poll and the new
  baseline are not delivered.
- **Rate limits.** Jira answers an exhausted hourly points quota, a burst limit,
  or a per-issue limit with 429 and `Retry-After`
  ([rate limiting](https://developer.atlassian.com/cloud/jira/platform/rate-limiting/)).
  A 429, or a transient 503 that carries `Retry-After`, pauses every binding of
  the connection for `Retry-After`; a 429 without it backs off exponentially.
  Other failures, including other 5xx, back off per binding. None of them
  moves the cursor.
- **Delivery.** Delivery is at least once. The Trigger event ID is a hash of the
  site, the issue ID, and the `updated` instant, so a refetch yields the same ID
  and a Flow target keyed by it starts one Flow per issue version.
- **Poll interval.** The connection's `pollInterval` setting is the delay
  between polls, at least 5 seconds. A poll normally makes one search request.

`jira.NewProjectIssueTriggerRunner` is the durable path. It loads each
binding's saved project, records every change in the binding's project Trigger
inbox, and commits the cursor in the binding's project poll cursor only after
that, so a restarted or replaced replica neither misses nor repeats a change.
From [`examples/issue-changed/main.go`](go/examples/issue-changed/main.go):

```go
triggerRunner, err := jira.NewProjectIssueTriggerRunner(project, issuechanged.ConnectionName, jira.ProjectIssueTriggerRunnerConfig{
	IssueChangedRoutes: []jira.ProjectIssueChangedTriggerRoute{{
		BindingName: issuechanged.IssueChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, issuechanged.AcceptEveryIssueChange, issuechanged.ResolveFlowID,
			issuechanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, jira.WithLogger(logger))
```

`jira.NewIssueTriggerRunner` takes the targets and cursor stores directly. A
route without a cursor store keeps the cursor in memory. The generated
`jira.NewIssueChangedTrigger` factory always does, because it receives no
project storage. Such a binding records a new baseline on every process start,
so changes made while the process is stopped are lost. Use them only for tests
and demonstrations.

Under `dexcli dev`, the Dex Web connections file keeps Trigger inboxes on disk,
but the poll cursor stays in process memory. A restarted local Worker therefore
records a new baseline. Hosted project storage keeps both.

The binding configuration is `jira.IssueChangedTriggerConfiguration`: the
`projectPicker` unit saves `projectId`, `projectKey`, and `projectName`. The ID
selects the project when it is set, so a renamed key keeps working. A typed key
without an ID selects by key. Restart the Worker after saving.

This release polls. Jira webhooks need a site administrator or a dynamic
webhook registration with a 30-day refresh, so they are not implemented yet.

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout. Redirects are never followed.
After a 401, the connector asks once for a refresh. Project storage refreshes
only when the recorded expiry has passed, and the connector then resends once,
because Jira rejects an unauthenticated request before acting on it; otherwise
the 401 selects `providerRejected`. A response that contains the access token
is never returned.

### searchIssues

Set exactly one of `Filter` and `JQL`. `IssueSearchFilter` turns typed values
into escaped, bounded JQL, so no caller text can change the query:

```go
jira.SearchIssuesInput{
    Filter: &jira.IssueSearchFilter{
        ProjectKeys:      []string{"OPS"},
        StatusCategories: []jira.StatusCategoryKey{jira.StatusCategoryToDo, jira.StatusCategoryInProgress},
        SummaryPhrase:    request.Summary,
    },
    PageSize: 20,
}
```

It sends
`project in ("OPS") AND statusCategory in (2, 4) AND summary ~ "\"Fire panel wiring\"" ORDER BY created DESC`.
Status categories use their IDs, which unlike names are not translated. For a
caller-built `JQL`, quote every outside value with `jira.QuoteJQLString`, which
escapes `\` and `"` and rejects control characters. Jira accepts only bounded
queries, so a filter needs at least one clause.

The request uses the enhanced search endpoint `POST /rest/api/3/search/jql`
with `PageSize` from 1 to 100 (zero requests 50) and continues with the
returned `NextPageToken`, which Jira expires after seven days. Each issue has
the standard fields plus up to 20 `AdditionalFields`, returned as raw JSON by
field ID. Search is eventually consistent: an issue created seconds ago can be
missing, so never use search to decide whether a create already happened.

### getIssue

`GetIssueInput.IssueIDOrKey` is a key such as `OPS-441` or a numeric ID. The
Value carries the standard fields, `Description` as plain text extracted from
Atlassian Document Format (at most 32767 characters, with
`IsDescriptionTruncated`), and requested `AdditionalFields`. Jira finds a moved
issue by its old key and returns the current key.

### createIssue

`CreateIssueInput` names exactly one of `ProjectKey` and `ProjectID`, exactly
one of `IssueTypeName` and `IssueTypeID`, a one-line `Summary` of at most 255
characters, an optional plain-text `Description`, `Labels` without whitespace,
and an optional `AssigneeAccountID`. The description is converted to a minimal
Atlassian Document Format document: blank lines separate paragraphs, other line
breaks become `hardBreak` nodes, and the text is never read as markup.

Jira has no idempotency key, so `createIssue` and `addComment` are `limited`
Mutations with the default send cap of 3 possibly effective POSTs per Call ID.
The shared single-dispatch fence counts each POST. A later attempt after a
possible earlier POST first reads back by the Call ID property, and sends again
only when nothing is found and the cap allows another POST:

| Outcome | Result |
| --- | --- |
| 201 with an issue ID and key | `created` |
| 400, 401, 403, 404, 411, 413, 414, 415, or 422 on the first POST | Checkpoint `canResend`, then `providerRejected`, with `RejectedFieldIDs` |
| 401 with a refreshable credential | Refresh the credential, checkpoint `canResend`, then Retry |
| One of those refusals after an earlier possible POST | `uncertain`, because the earlier POST may have created the issue |
| 429 | Checkpoint `canResend`, then Retry after `Retry-After` |
| DNS, connect, or TLS failure before any connection opened | Checkpoint `canResend`, then Retry |
| 503 | Checkpoint `canResend`, then Retry after `Retry-After` |
| Timeout, dropped connection, 3xx, 408, 409, another 4xx, or another 5xx after dispatch | Read back; confirmed creation selects `created`, otherwise Retry and resend below the cap, or `uncertain` at the cap |
| 2xx whose body is oversized, unreadable, unusable, or reflects the token | Read back; confirmed creation selects `created`, otherwise Retry and resend below the cap, or `uncertain` at the cap |
| Unreadable or untrusted dispatch checkpoint | Retry without sending |

Once Jira has returned an issue ID, a later attempt reads that issue back and
never sends again; a failed confirmation selects `uncertain`. During read-back,
a transient 429, 5xx, transport error, interrupted response, or temporarily
unavailable credential returns Retry without another POST. A transient
credential lookup failure before a write also retries after recording
`notSent`. Reauthorization-required or invalid credentials select `defect`
before the first dispatch and `uncertain` after an earlier possible POST.
Set the Step's `MaxSends` to 1 to forbid any resend. If the retry budget is
exhausted, the Flow fails.

On `providerRejected` and `uncertain`, the Value echoes the requested project
and summary without an issue key.

### transitionIssue

`TransitionIssueInput` selects one available transition by any combination of
`TransitionID`, `TransitionName`, and `DestinationStatusName`; every one that is
set must match, names without case. `ResolutionName` fills a transition
screen's resolution. The operation:

1. reads the issue's status and available transitions in one request
   (`fields=status&expand=transitions`);
2. selects `transitioned` without sending anything when the issue is already in
   `DestinationStatusName`, or the matched transition leads to the current
   status;
3. selects `transitionUnavailable` when no available transition matches, or
   several do, with the current status and up to 50 available transitions;
4. performs the transition, and after any response other than 204, 404, or
   429 reads the issue again. When the issue reached the destination, the
   result is `transitioned`. Otherwise a conclusive 4xx selects
   `providerRejected` with the field IDs, and a 409, 5xx, or dropped connection
   retries.

Jira refuses a transition that is not available from the issue's current
status, so a repeated or concurrent attempt cannot move an issue twice. Set
`DestinationStatusName` whenever the destination is known: a retried or backup
attempt then recognizes a transition Jira already made. With only a transition
ID or name, such an attempt finds the transition no longer offered and selects
`transitionUnavailable` with `Status` already at the destination.

### addComment

`AddCommentInput` names the issue and a plain-text `Body`, converted like the
description. The branch table matches `createIssue`, except that a 404 selects
`notFound`.

## Avoiding duplicate issues and comments

`createIssue` and `addComment` use sync Execute durability even though a
request is usually fast. Sync persists a result before the Flow advances;
async may replay one lost before its batched commit. Both modes use the same
attempt and retry rules. The create test records one request and one issue;
the timeout tests check that an unconfirmed write is resent at most up to the
send cap before `uncertain`. Keep these Steps sync to reduce replay risk.

`transitionIssue` keeps the async default. A lost Worker after Jira applies
the transition causes another Execute; it reads the destination status and
does not send the transition again. The integration test checks one POST.

Each create and comment includes a non-secret hash of the stable Call ID in a
Jira entity property. The released `sdkgo/singledispatch` fence owns the Dex
heartbeat claim, send count, and send window. A missing or stale checkpoint
counts each earlier attempt as a possible POST, so another POST follows only
an empty read-back below the send cap; a foreign or unreadable checkpoint
returns Retry without sending. When a response supplies a resource ID, the
fence saves it for direct read-back; otherwise the connector searches a
bounded page for the property. A transient lookup retries without sending.
The property is not an Atlassian idempotency key, and the bounded search can
miss a write Jira has not indexed yet, so a resend may duplicate an issue or
comment; at most the send cap of them exist. When the cap is reached and
read-back cannot confirm the write, the Step selects `uncertain`.

A traced DNS, connect, or TLS failure before any connection is obtained, HTTP
429, and HTTP 503 select Retry after the fence records permission for the next
attempt. The fence reserves part of the Execute budget for read-back and
classification. A 409 reads back before choosing a result.
An application handles unresolved `uncertain` without creating again; the
[`triage-issue`](go/examples/triage-issue) example parks the create for an
operator, who confirms the issue found in the project or approves a new create.

Atlassian documents issue and comment entity properties and their read APIs:
[Create issue](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issues/),
[Enhanced issue search](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-search/),
[Get issue property](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-properties/),
[Add and list comments](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comments/),
and [Get comment property](https://developer.atlassian.com/cloud/jira/platform/rest/v3/api-group-issue-comment-properties/).
Live behavior of the marker on Jira Cloud remains unverified.
The comment-list and single-comment GET `expand=properties` parameters are
undocumented; the connector falls back to the documented comment-property GET
when expansion omits the property.

## Mocks

`jiramock` supplies a consistent OPS triage world: an empty initial search,
creation and read-back of OPS-4, comment 10501, and a move to In Progress.
Each operation also has mocks for its definite business-outcome branches.
The runnable example's real-Dex mock test starts with:

```go
mock := jiramock.New(t, ConnectionName)
flow := NewFlow(mock.Connection(), ProjectSelection{})
```

The test asserts literal call inputs and counts for all five operations.

## Not in this release

- Webhook Triggers for issue and comment events, and a comment Trigger.
- API-token (HTTP Basic) authentication, which Studio setup commands cannot
  send.
- Custom field writes, priorities, parents, and an issue type picker.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, RPC, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/atlassian/jira/connector.yaml
go run ./cmd/connectorctl generate --check connectors/atlassian/jira/connector.yaml
(cd connectors/atlassian/jira/ui && npm ci && npm test && npm run build)
```

The `issueChanged` tests run against `internal/jirasearchfake`, a local
enhanced search stand-in. They cover the baseline, ascending delivery with
stable IDs, project filtering, a late-indexed update within the lookback,
paging, an update that shifts a paged backlog, a rejected and a stale page
token, a host clock ahead of Jira, 429 and 503 with `Retry-After`, and resets
for another site or project. The
[`issue-changed`](go/examples/issue-changed) integration tests run them through
a real Dex Worker, including a restart and a changed project.

The deterministic fakes cover every branch above, JQL escaping and injection
attempts, ADF conversion both ways, one refresh after a 401, a second 401,
rate limits with `Retry-After`, redirects, oversized and malformed responses,
token reflection, site resolution, and the refresh driver's JSON grant,
rotation, 403 `invalid_grant`, outage, and scope checks.

No live Atlassian credentials were used. The following live behavior is
unverified: Dex Web's form-encoded authorization-code exchange against
Atlassian's token endpoint, which documents JSON only; whether the returned
`scope` lists every requested scope, including `offline_access`, which Dex Web
requires; a `http://127.0.0.1` Redirect URI in the developer console; the
refresh exchange and 403 `invalid_grant` shape; real response shapes of
enhanced search, issue, create, transition, and comment endpoints; enhanced
search `nextPageToken` paging and consistency delay; the `summary ~` phrase
search and status category ID clauses; workflow and transition-screen rejections;
429 and 5xx behavior under load; the `accessible-resources` and
`project/search` commands through Dex Web; and the developer-console paths in
the setup guidance. Entity-property writes during issue/comment creation,
property read-back, the undocumented comment-list and single-comment property
expansions, and 503 responses under load are also unverified live. For
`issueChanged`, relative `updated >= -Nm` bounds, `ORDER BY updated ASC`
paging, the search index delay, and 400 for an expired page token are
unverified against live Jira.
