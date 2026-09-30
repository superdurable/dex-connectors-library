# Jira Cloud Connector

This module searches, reads, creates, transitions, and comments on Jira Cloud
issues through Atlassian's OAuth 2.0 (3LO) gateway,
`https://api.atlassian.com/ex/jira/{cloudId}/rest/api/3`:

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

No Trigger ships in this release. Jira webhooks need the shared webhook source
design; until it lands, poll with `searchIssues`.

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
(`action=create`). It stores `projectId`, `projectKey`, and `projectName`.

Dex Web `cli-v1.1.0` sends no connection configuration to a unit, so the unit
cannot read the connection's `cloudId`. It asks for a site `cloudId` used only
for listing, and it always accepts a typed project key. Studio commands send
the stored access token, which Dex Web does not refresh; a running Worker
refreshes and persists it, and otherwise **Reconnect** issues a new one.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "jira",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/atlassian/jira",
    "moduleVersion": "v0.1.0",
    "provider": "atlassian",
    "connectionName": "jira-triage",
    "configuration": {"cloudId": "1324a887-45db-1bf4-1e99-ef0ff456d421"},
    "credentials": {"oauth_client_id": "...", "oauth_client_secret": "...", "access_token": "...", "refresh_token": "..."}
  }]
}
```

Load it with `localconfig.LoadFromEnvironment` and
`jira.NewLocalConnection(store, "jira-triage")`. Credentials are reread and
refreshed before every provider call; `cloudId`, `endpoint`, and
`maxResponseBytes` are startup configuration.

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider and `DecodeResolvedCredentialsJSON`, which
accepts only `access_token`:

```go
provider, err := hostedconfig.NewCredentialProviderFromEnvironment(jira.ConnectorID, "jira-triage", jira.DecodeResolvedCredentialsJSON)
if err != nil {
    return err
}
client, err := jira.New(jira.Config{CloudID: cloudID}, provider)
```

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout. Redirects are never followed.
After a 401, the connector refreshes the credential once and resends once,
because Jira rejects an unauthenticated request before acting on it. A
response that contains the access token is never returned.

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

Jira has no idempotency key, so the connector retries only when Jira cannot have
created anything:

| Outcome | Result |
| --- | --- |
| 201 with an issue ID and key | `created` |
| 400, 401, 403, 404, 409, 413, 422, or another 4xx except 408 and 429 | `providerRejected`, with `RejectedFieldIDs` |
| 429 | Retry, after `Retry-After` |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 408, 3xx, or 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, unusable, or reflects the token | `uncertain` |

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
request is usually fast. With async durability, Dex runs the Step in a local
phase of about seven seconds and then dispatches a fallback attempt, so a slow
request is sent a second time while the first is still in flight. A real Dex
run with a Jira fake that held the create response for nine seconds recorded
two create requests under async durability and one under sync;
`TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex` guards this. Do not
override these Steps to async.

`transitionIssue` keeps the async default. The same nine-second fake holding the
transition response recorded one transition request: the fallback attempt read
the issue, found it in the destination status, and sent nothing;
`TestSlowTransitionBackupAttemptMovesTheIssueOnceWithRealDex` guards this.

The connector never retries a dispatched create or comment, but Dex runs a Step
at least once. If the Worker is lost after Jira accepts a create and before Dex
commits the Step result, Dex runs the Step again and Jira creates a second
issue. No connector can close this window without a provider idempotency key.
An application handles `uncertain` without creating again automatically; the
[`triage-issue`](examples/triage-issue) example parks the create for an
operator, who confirms the issue found in the project or approves a new create.

## Not in this release

- Triggers for issue and comment events, which need the shared webhook source.
- API-token (HTTP Basic) authentication, which Studio setup commands cannot
  send.
- Custom field writes, priorities, parents, and an issue type picker.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example owns its real
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
the setup guidance.
