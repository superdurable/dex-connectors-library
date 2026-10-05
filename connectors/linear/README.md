# Linear Connector

> **Verification status: partial live.** Only unauthenticated and invalid-credential probes reached Linear's
> GraphQL API and OAuth token endpoint; everything else ran on a real Dex stack against a local stand-in.
> No Linear workspace was used. See [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Linear Connector reads and writes Linear issues from Dex Flows through Linear's GraphQL API at
`https://api.linear.app/graphql`, and receives Linear's signed Issue webhooks. It covers Team → workflow
state → Issue, with assignee resolution by email, label set operations, and comments.

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `linear.NewSearchIssuesStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `linear.NewGetIssueStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `linear.NewCreateIssueStep` | Mutation | async | `created` | `providerRejected`, `defect` |
| `linear.NewUpdateIssueStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `linear.NewAddCommentStep` | Mutation | async | `added` | `notFound`, `providerRejected`, `defect` |
| `linear.NewFindUserByEmailStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `linear.NewListWorkflowStatesStep` | Query | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

| Trigger | Event | Event ID |
| --- | --- | --- |
| `linear.NewIssueEventReceivedTrigger` | `IssueEvent`: a signed Issue `create`, `update`, or `remove` webhook | `<action>:<issue UUID>:<updatedAt in Unix ms>` |

Only the happy-path branch of each operation is required; every other branch is optional, and an unwired
optional branch fails the Flow. Every operation uses a 30-second Execute timeout and a six-attempt, five-minute
retry window. No write has an `uncertain` branch: creates carry a client-supplied UUID and updates are
absolute, so every ambiguous outcome is retried safely; see [Duplicate safety](#duplicate-safety).

Workflow state names, types, label names, and priorities are Linear's own values, passed through unchanged.
`WorkflowStateType` (`triage`, `backlog`, `unstarted`, `started`, `completed`, `canceled`, `duplicate`) is
the portable signal; a type Linear adds later reaches the application as returned.

## Linear setup

A connection uses one of two authentication methods.

### Personal API key (default, recommended)

Open <https://linear.app/settings/account/security>, choose **Personal API keys > New API key**, give the key
**Read** and **Write** permission and access to the teams the Flows use, and copy the `lin_api_` key into
`api_key`. Issues and comments the connector writes are authored by the key's user. The key is sent as the raw
`Authorization` header, as Linear documents; an invalid key in the `lin_api_` format sent behind `Bearer`
was answered `INPUT_ERROR` (HTTP 400) asking to remove the `Bearer` prefix (probed live). A workspace
admin may need to allow members to create API keys. Requests by one user share Linear's per-user budget of
2,500 requests and 3,000,000 complexity points per hour, even across keys.

### Linear OAuth

Create an OAuth application at <https://linear.app/settings/api/applications/new>, add the Redirect URI that
Dex Web shows to its callback URLs, and copy the Client ID and Client secret. The connection uses:

- authorization at `https://linear.app/oauth/authorize` with `prompt=consent`, so each Connect can pick a
  workspace, and S256 PKCE, which Linear supports;
- code exchange and refresh at `https://api.linear.app/oauth/token`, form encoded as Linear requires.

The manifest requests the single scope `write`. Linear documents the `scope` parameter as a comma-separated
list, and OAuth clients such as Dex Web conventionally separate scopes with spaces; one scope sidesteps any
separator difference, and Linear documents that `read` is always granted. The token response's `scope` (`"read write"`) then contains every manifest scope, as Dex Web
requires.

Since Linear's April 1, 2026 migration, every OAuth application receives access tokens that expire after
24 hours (`expires_in: 86399`) and a refresh token that rotates on each refresh. `CredentialRefreshDriver`
refreshes through `sdkgo/oauthtoken` with `client_secret_post`, five minutes before the recorded expiry, and
stores the rotated refresh token with a future expiry from `expires_in`. Linear accepts the old refresh token
again for 30 minutes when the answer to a refresh is lost. `invalid_grant`, `invalid_client`, and
`unauthorized_client`, or a refreshed grant without `write`, require reauthorization; any other failure keeps
the connection and retries. After Linear rejects an OAuth access token with HTTP 401, the connector asks once
for a refresh, which project storage performs only when the stored expiry has passed, and resends once; a 401
means Linear ran nothing.

Linear documents that OAuth applications created before December 1, 2023 return `scope` as a JSON array.
`sdkgo/oauthtoken` reads `scope` as a string, so a refresh for such an application fails and is retried until
the token expires; create a new OAuth application instead. Linear's `client_credentials` grant (30-day app
tokens without a refresh token) and `actor=app` are not declared.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in the project configuration. The
application loads that configuration once and opens the connection by the name it declares, as
[`examples/issue-request/main.go`](examples/issue-request/main.go) does:

```go
	project, err := projectconfig.LoadFromEnvironment(ctx)
	if err != nil {
		return err
	}
	connection, err := linear.NewProjectConnection(project, issuerequest.ConnectionName, connectionOptions()...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Credentials stay in project storage and are read
for every provider call and every webhook delivery; `maxResponseBytes`, `webhookMaxBodyBytes`, and
`webhookSignatureTolerance` are startup configuration.

## Operations

Every operation sends one named GraphQL document per request, such as `query LinearSearchIssues`, with its
values in `variables`, so input never becomes query text. Requests are bounded by 12 seconds each and 25
seconds per operation, below the 30-second Execute timeout, and redirects are never followed. IDs are Linear
UUIDs; an issue may also be named by its identifier, such as `ENG-123`.

### searchIssues

`searchIssues` reads `issues(filter, first, after, orderBy, includeArchived)`, Linear's filtered list, not its
relevance-ranked `searchIssues` full-text query. `IssueSearchFilter` builds Linear's `IssueFilter` from typed
fields; every set field must match and lists match any value:

| Field | Linear filter |
| --- | --- |
| `TeamID` or `TeamKey` | `team: {id: {eq}}` or `team: {key: {eq}}` |
| `StateTypes`, `StateIDs` | `state: {type: {in}, id: {in}}` |
| `AssigneeID`, `AssigneeEmail`, `IsUnassigned` | `assignee: {id: {eq}}`, `assignee: {email: {eqIgnoreCase}}`, `assignee: {null: true}` |
| `LabelIDs`, `LabelNames` | `labels: {some: {id: {in}, name: {in}}}` |
| `ProjectID` | `project: {id: {eq}}` |
| `Title`, `TitleContains` | `title: {eq}`, `title: {containsIgnoreCase}` |
| `UpdatedAfter`, `CreatedAfter` | `updatedAt: {gt}`, `createdAt: {gt}` in UTC milliseconds |

`PageSize` is 1 to 100 (`MaxSearchPageSize`), 50 by default as in Linear. `Order` is `createdAt` (Linear's
default) or `updatedAt`. A later page repeats the same filter with the previous `NextCursor`, Linear's
`pageInfo.endCursor`; an empty `NextCursor` ends the search. Each result is an `IssueSummary`: IDs, identifier,
title, URL, team, workflow state, assignee ID, priority, label IDs, due date, and timestamps. Summaries select
scalar lists instead of nested connections, so a 100-issue page stays far below Linear's 10,000-point limit
for one query.

For changed-since polling, set `UpdatedAfter` to the last watermark and `Order: updatedAt`, read every page,
and keep the largest `UpdatedAt` seen as the next watermark. Linear discourages polling; prefer the
`issueEventReceived` Trigger.

### getIssue

`getIssue` filters the issues list by `id` or by team key and number, with `includeArchived: true`, instead of
calling `issue(id:)`: an empty list is `notFound`, so the branch does not depend on an error code Linear does
not document. An archived issue is found with `ArchivedAt` set; a deleted or trashed one, or one the
connection cannot see, is `notFound`. An identifier finds an issue only under its current team key; after a
move to another team, use the UUID. The `Issue` adds the Markdown description (cut at 32,768 characters,
`MaxDescriptionCharacters`, with `IsDescriptionTruncated`), priority label, estimate, assignee and creator
names (never emails), up to 50 labels, project, cycle, parent, branch name, and started, completed, and
canceled times.

### createIssue

`createIssue` sends `issueCreate(input: {id, teamId, title, …})`. `Title` is one line of at most 255 characters
(`MaxIssueTitleCharacters`, a connector bound). `Description` is Markdown of at most 65,536 characters.
`StateID`, `AssigneeID`, `LabelIDs` (at most 50), `Priority` (0 none to 4 low), `DueDate` (`YYYY-MM-DD`),
`ProjectID`, and `ParentID` (UUID or identifier) are optional. The result is the created `IssueSummary` with
`IssueID` and `IsReplayed`; on `providerRejected` the requested team and title are echoed without an ID.

### updateIssue

`updateIssue` sends `issueUpdate(id, input)` with at least one change: `StateID` moves the issue to a workflow
state of its team, `AssigneeID` or `Unassigns`, `AddedLabelIDs` and `RemovedLabelIDs` (Linear's set
operations, which keep labels the Step does not name), `Priority`, `DueDate` or `ClearsDueDate`, and `Title`.
Every value is absolute, so a repeated update leaves the issue unchanged. Linear lets an issue move to any
state of its team; there is no transition graph to consult. After an input rejection the connector reads the
issue: a missing issue selects `notFound`, any other rejection `providerRejected`.

### addComment

`addComment` sends `commentCreate(input: {id, issueId, body})` with Markdown of 1 to 65,536 characters. After
a rejection with no comment under the Step's UUID, the connector reads the issue to choose `notFound` or
`providerRejected`.

### findUserByEmail

`findUserByEmail` reads `users(filter: {email: {eqIgnoreCase}}, includeDisabled)` and returns the `User` with
its UUID, name, display name, email, and active, admin, and guest flags. No match is `notFound`; two matches,
or a user with another email, is `invalidResponse`. Disabled users are found only with `IncludesDisabled`.

### listWorkflowStates

`listWorkflowStates` reads `workflowStates(filter: {team: {id: {eq}}})` and orders the states by type, from
`triage` to `duplicate`, then by board position. Every team has states, so an empty list is `notFound`.
`StateNamed` and `FirstStateOfType` choose a state by name or category.

## Duplicate safety

Linear documents a client-supplied `id`, "the identifier in UUID v4 format", on `IssueCreateInput` and
`CommentCreateInput`. `createIssue` and `addComment` derive that UUID from the Dex Call ID with SHA-256 and the
version 4 and variant bits set. Every attempt of one Step execution, including a re-dispatched local-phase
attempt or one on a replacement Worker, sends the same UUID; every other Step execution sends another. The
issue and the comment of one Step never share a UUID.

After any answer other than a usable record, a rate limit, a credential or permission rejection (an
authentication or `FORBIDDEN` error, HTTP 401 or 403), or a GraphQL request Linear could not parse or
validate, the connector reads the record back by that UUID before deciding: a record that exists selects
`created` or `added` with `IsReplayed`. Otherwise an availability failure, a lost connection, an unusable
answer, or a read-back that fails is retried with the same UUID, and a conclusive rejection selects
`providerRejected`. Linear does not document which error a repeated `id` returns, so the connector does not
rely on its code; if Linear answered a repeated `id` with one of the errors that are not read back, a
`providerRejected` create or comment could hide a record an earlier attempt stored. This lets both writes keep async durability:
`TestSlowCreateIsDispatchedAgainAndCreatesOneIssueWithRealDex` holds Linear's answer for nine seconds, Dex
dispatches the Step again, and the second attempt reads back the issue the first stored; the same holds for
comments and for a Worker lost mid-create. `updateIssue` needs no key: its values are absolute.

The guarantee assumes that Linear refuses a second record with an existing `id` and that a read by `id`
directly after a write finds the record; neither was verified against Linear. Linear has no conditional
update, so `updateIssue` overwrites a concurrent change to the fields it sets.

## Errors

Linear answers GraphQL errors in an `errors` array with `extensions.code` and `extensions.type`, often with
HTTP 400 even for a rate limit. A Failure never carries Linear's `message` or `userPresentableMessage`; it
repeats only the code and a known type, such as `Linear rejected the input (HTTP 400) [INPUT_ERROR; invalid
input]`. The code and type decide before the HTTP status.

| Response | Result |
| --- | --- |
| `RATELIMITED`, type `ratelimited`, 429 | Retry after `Retry-After` when sent, otherwise the Step's backoff; Linear refills its leaky-bucket budget continuously |
| `INTERNAL_SERVER_ERROR`, types `internal error`, `lock timeout`, `network error`, 408, 5xx, connection or read failure | Retry; a create or comment is read back first |
| `AUTHENTICATION_ERROR`, type `authentication error`, 401 | `providerRejected`, `AUTHENTICATION`; an OAuth 401 is refreshed and resent once first |
| `FORBIDDEN`, types `forbidden`, `feature not accessible`, 403 | `providerRejected`, `AUTHORIZATION` |
| type `usage limit exceeded` | `providerRejected`, `QUOTA_EXHAUSTED`; a write is read back first |
| `INPUT_ERROR`, `BAD_USER_INPUT`, types `invalid input`, `user error` | `providerRejected`, `VALIDATION`; a write is read back first |
| `GRAPHQL_PARSE_FAILED`, `GRAPHQL_VALIDATION_FAILED`, `BAD_REQUEST`, type `graphql error`, bare 400 | `defect`, `PROTOCOL` |
| any other code, 3xx | `providerRejected`; a write is read back first after another code |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` for reads and `updateIssue`; a create or comment is read back and otherwise retried |
| invalid input or connection credentials | `defect`, with no request |

A mutation whose payload returned a usable record beside a nested field error selects its happy branch. The
Receipt carries the Call ID, the record ID, and Linear's `X-Request-Id`.

## Issue webhooks

`issueEventReceived` is built on `sdkgo/webhooktrigger`. `Connection.IssueEventReceivedWebhookHandler` returns
the connection's one endpoint, and `NewProjectIssueEventReceivedEndpointRunner` combines it with the bindings
that the project configuration stores, wrapping each target in the binding's durable project inbox.

Create the webhook in Linear under **Settings > API > Webhooks > New webhook** (workspace admins only), or on
the OAuth application's page, with the public HTTPS URL where the application mounts the handler and the
**Issues** data change event. Copy its signing secret into `webhook_signing_secret`. Linear sends no
verification handshake.

For each delivery the endpoint:

1. accepts only `POST` with a body of at most `webhookMaxBodyBytes`, otherwise 405 or 413;
2. answers 503, so Linear retries, while `webhook_signing_secret` is blank or no binding runs;
3. checks `Linear-Signature`, the hex HMAC-SHA256 of the raw body under the signing secret, in constant time,
   and then the signed `webhookTimestamp` in the body, which must be within `webhookSignatureTolerance` (one
   minute by default, Linear's recommendation) of the server clock; a forged, tampered, stale, or future
   delivery is answered 400;
4. acknowledges an event of another entity type, such as `Comment`, with 200 and records nothing;
5. records an Issue event in every binding whose configuration accepts it, and answers 200 only after that.

Linear documents `webhookTimestamp` as the time it sent the delivery, so a retry carries a new one and a new
signature; the event ID therefore comes from the change itself:
`<action>:<issue UUID>:<updatedAt in Unix milliseconds>`. A redelivery of one change keeps its ID, and a
Flow-start target starts one Flow for it. `IssueEvent` carries the action, the actor (ID, name, type, never an
email), the issue without its description, the names of the properties an update changed, and the previous
workflow state. Linear retries a failed delivery after 1 minute, 1 hour, and 6 hours and may disable a webhook
that keeps failing.

`IssueEventReceivedTriggerConfiguration` filters what a binding records: `Actions` (`create`, `update`,
`remove`; empty accepts all) and `TeamID` (blank accepts every team), which the `teamPicker` unit can fill.
The application's `TriggerFilter` remains the admission rule.

## Studio team picker

Without an `apollo-require-preflight` header, Linear's Apollo server blocks a GraphQL `GET` as a possible
CSRF (`BAD_REQUEST`); with it, an unauthenticated `GET` reaches authentication (`AUTHENTICATION_ERROR`). Both
were probed live without credentials; a `GET` with a valid token was not tried. The read-only `listTeams` command
therefore sends `GET https://api.linear.app/graphql?query=query LinearTeamPicker { teams(first: 100) … }` with
that fixed header, and the `teamPicker` unit stores `teamId`, `teamKey`, and `teamName`.

Studio commands send a credential only as `Bearer` or in a header other than `Authorization`, and Linear
rejects a personal API key behind `Bearer`. The command therefore uses the OAuth `access_token`: for a
personal-API-key connection it has no credential and fails, and the unit offers manual entry of the team UUID
(**Settings > Teams > the team > Copy team ID**). The command lists at most 100 teams and says when more
exist. Studio commands send the stored access token, which Dex Web does not refresh; the Worker's next Linear
call refreshes it.

## Not in this release

- Project, cycle, and label pickers, and label or project creation.
- Comment, Project, and Cycle webhook Triggers; the endpoint acknowledges those entity types without a record.
- Linear's `client_credentials` grant and `actor=app` authorization for agent applications.
- Attachments, sub-issue relations other than `ParentID`, and custom views.

## Examples

- [`examples/issue-request`](examples/issue-request) is a Dex Web **Start Flow** example that uses all seven
  operations and the `teamPicker` unit: it resolves the assignee by email, reuses the team's open issue with
  the requested title or creates one, moves it to the requested workflow state, adds one comment, and reads it
  back.
- [`examples/issue-events`](examples/issue-events) serves the webhook endpoint and starts one Flow per created
  issue, which reads the issue back with `getIssue`.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the examples own their real Worker, retry, duplicate-dispatch,
Worker-loss, Trigger delivery, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/linear/connector.yaml
go run ./cmd/connectorctl generate --check connectors/linear/connector.yaml
(cd connectors/linear/ui && npm ci && npm test && npm run build)
```

The provider fakes cover the raw key and Bearer headers, named POST-only GraphQL with variables, every error
mapping without message text, Linear's HTTP 400 rate limit and `Retry-After`, redirects, oversized, malformed,
and credential-reflecting answers, the client UUID's stability and read-back after every ambiguous answer, the
OAuth refresh with rotation, scope checks, `invalid_grant`, and one resend after a 401, and webhook signature,
timestamp, filter, and durable-inbox replay behavior.

Probed live without valid credentials on 2026-10-04 and 2026-10-05, the only contact with Linear: a `POST`
query without `Authorization` and one with an invalid raw key returned `AUTHENTICATION_ERROR` (HTTP 401); an
invalid key in the `lin_api_` format behind `Bearer` returned `INPUT_ERROR` (HTTP 400), while a shorter
invalid value behind `Bearer` returned `AUTHENTICATION_ERROR`; an unauthenticated `GET` query returned the
CSRF `BAD_REQUEST` without `apollo-require-preflight` and `AUTHENTICATION_ERROR` with it; the token endpoint
answered a request from an unknown client with HTTP 400 `invalid_client`.

No Linear workspace was used. The following live behavior is unverified:

- which error Linear returns for a repeated client-supplied `id`, that it never stores a second record with
  one, and that a read by `id` directly after a write finds the record;
- whether Linear accepts the derived UUID, which has v4 version and variant bits but deterministic content;
- the response shapes of `issues`, `issueCreate`, `issueUpdate`, `commentCreate`, `comments`, `users`, and
  `workflowStates`, and the codes of entity-not-found and validation errors;
- whether `labels: {some: …}` and `assignee: {email: {eqIgnoreCase}}` filter as documented, and the largest
  page Linear allows;
- Dex Web's authorization request against Linear's comma-separated `scope` parameter, its form-encoded code
  exchange, PKCE, `prompt=consent`, and the real refresh rotation and 30-minute grace period;
- Linear's webhook payloads, whether `updatedAt` changes for every update event, the retry schedule, and the
  stability of `Linear-Delivery`, which the connector does not use;
- the `listTeams` command and team picker through Dex Web, and the settings paths in the setup guidance.
