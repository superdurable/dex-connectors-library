# Jira Service Management Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Jira Service Management; no live Atlassian site was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module raises, reads, labels, comments on, and moves Jira Service
Management customer requests as tickets through Atlassian's OAuth 2.0 (3LO)
gateway, `https://api.atlassian.com/ex/jira/{cloudId}`. Request, comment, SLA,
and customer calls use the request API at `/rest/servicedeskapi`; search,
labels, priority, and workflow transitions use the Jira platform API at
`/rest/api/3` on the same site, because every customer request is a Jira issue.

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `findCustomerByEmail` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `searchTickets` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getTicket` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createTicket` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `updateTicket` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `transitionTicket` | Mutation | async | `transitioned` | `notFound`, `transitionUnavailable`, `providerRejected`, `invalidResponse`, `defect` |
| `addComment` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy branch of each operation is required; every other branch is
optional, and an unwired optional branch fails the Flow. Operation, branch, and
record names follow the desks on main (`searchTickets`, `getTicket`,
`createTicket`, `updateTicket`, `addComment`, `findCustomerByEmail`; `searched`,
`found`, `created`, `updated`, `added`, `uncertain`), so a process can swap desks
without a per-vendor branch. `transitionTicket` is the one addition, because a
Jira workflow can refuse a status change that another desk would accept.

## Ticket overlay

| Concern | How this connector answers it |
| --- | --- |
| Status | `Ticket.Status` is the site's own workflow status with Jira's fixed `CategoryKey` (`new`, `indeterminate`, `done`), never mapped to another vocabulary. `createTicket` also returns the request API's `RequestStatus`, whose `Category` is `NEW`, `INDETERMINATE`, `DONE`, or `UNDEFINED`: the same three categories in upper case. Status changes go through `transitionTicket`. |
| Public reply or internal note | `addComment` sends `public: true` for a reply the customer sees and is notified of, and `public: false` for an internal note only agents see. `getTicket` returns each comment's `IsPublic`, and a comment without its visibility selects `invalidResponse` rather than risk showing a note as a reply. |
| Requester | `findCustomerByEmail` matches the service desk's customers by exact address; `createTicket` raises the request on the customer's behalf with `RaiseOnBehalfOfAccountID`. Results carry account IDs and display names, never email addresses. |
| SLA | `getTicket` reads the request's SLAs: each SLA's ongoing cycle and up to 20 completed cycles, with start, breach, and stop times, `IsBreached`, `IsPaused`, `IsWithinCalendarHours`, and goal, elapsed, and remaining milliseconds. |
| Tags | Labels. `updateTicket` adds and removes labels with Jira's `add` and `remove` verbs, so concurrent changes to other labels survive, and `searchTickets` filters by label. |

No Trigger ships in this release; see [Not in this release](#not-in-this-release).

## Atlassian setup

Create one OAuth 2.0 (3LO) app at
<https://developer.atlassian.com/console/myapps/>:

1. **Create > OAuth 2.0 integration**, name the app, and accept the terms.
2. **Permissions > Jira Service Management API > Add**, then **Configure** and
   add the classic scopes `read:servicedesk-request`,
   `write:servicedesk-request`, and `manage:servicedesk-customer`.
3. **Permissions > Jira API > Add**, then **Configure** and add the classic
   scopes `read:jira-work` and `write:jira-work`. The connection also requests
   `offline_access`, which makes Atlassian issue a rotating refresh token.
4. **Authorization > OAuth 2.0 (3LO) > Configure**: paste the Redirect URI Dex
   Web shows into **Callback URL** and save.
5. **Settings**: copy **Client ID** and **Secret** into the Dex Web form.
6. Choose **Connect** while signed in as a licensed agent of the service desks
   the Flows use, pick the site on the consent screen, and accept.

Atlassian's REST reference names the scope each call needs:

| Call | Classic scope |
| --- | --- |
| Request create, read, comments, customer list of a desk | `read:servicedesk-request`, `write:servicedesk-request`, `manage:servicedesk-customer` |
| `GET /rest/servicedeskapi/request/{key}/sla` | `read:jira-work` |
| Enhanced search, issue read, edit, and transitions | `read:jira-work`, `write:jira-work` |

The customer list (`GET /rest/servicedeskapi/servicedesk/{serviceDeskId}/customer`)
is an experimental Atlassian method, so `findCustomerByEmail` sends
`X-ExperimentalApi: opt-in`; no other operation uses an experimental method.
Atlassian also publishes granular scopes such as
`write:request.comment:jira-service-management`, still marked beta; this
release requests only classic scopes.

Dex requests `audience=api.atlassian.com` and `prompt=consent` without PKCE,
as the Jira connector does. `CredentialRefreshDriver` refreshes the one-hour
access token through `sdkgo/oauthtoken` with a JSON body and
`client_secret_post`, stores every rotated refresh token, reports
reauthorization on Atlassian's HTTP 403 `invalid_grant`, and requires
reauthorization when a refresh drops any of the five scopes above.

### Choosing the site

Every operation works on one site, named by the non-secret `cloudId`
configuration (a UUID), which the `sitePicker` unit fills from the read-only
`listAccessibleSites` command. When `cloudId` is blank, the connector calls
`accessible-resources` before its first operation and uses the only site whose
grant carries a service desk scope; it selects `defect` for none or several and
caches the site until restart. Dex Web renders a connection field's
`studioUnit` as text and rejects Atlassian's top-level array, so the unit always
offers manual entry of the `cloudId` shown at
`https://<your-site>.atlassian.net/_edge/tenant_info`, as in the Jira connector.

### Choosing a service desk and request type

Two configuration units list the agent's desks and request types through fixed
`https://api.atlassian.com` commands; only the `cloudId` and `serviceDeskId`
path segments are declared parameters, and both responses are paged JSON
objects (`{"values": [...]}`) that Dex Web accepts:

- `serviceDeskPicker` (`listServiceDesks`,
  `GET /ex/jira/{cloudId}/rest/servicedeskapi/servicedesk`) saves
  `serviceDeskId`, `projectKey`, and `serviceDeskName`.
- `requestTypePicker` (`listServiceDesks`, then `listRequestTypes`,
  `GET /ex/jira/{cloudId}/rest/servicedeskapi/servicedesk/{serviceDeskId}/requesttype`)
  saves `serviceDeskId`, `requestTypeId`, and `requestTypeName`.

Both accept typed IDs, page with `start`, and ask for a listing `cloudId` when
the host reports none. Studio commands send the stored access token, which Dex
Web does not refresh; the Worker's next call refreshes it.

## Project configuration

The application loads the project configuration once and opens the connection
by the name it declares, as
[`examples/support-request/main.go`](examples/support-request/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := jiraservicemanagement.NewProjectConnection(project, supportrequest.ConnectionName)
if err != nil {
	return err
}
```

Credentials stay in project storage, are read for every provider call, and are
refreshed first when the access token is missing, has no recorded expiry, or
expires within five minutes; `cloudId`, `endpoint`, and `maxResponseBytes` are
startup configuration.

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout. Redirects are never followed.
After a 401 the connector asks once for a refresh and resends once, because
Atlassian rejects an unauthenticated request before acting on it. A response
that contains the access token is never returned.

### findCustomerByEmail

Reads one page of 50 of the service desk's customers with
`GET /rest/servicedeskapi/servicedesk/{serviceDeskId}/customer?query={email}`,
which matches display names, names, and addresses as text, and keeps only
customers whose `emailAddress` equals the address without case. `found` lists
them with `AccountID`, `DisplayName`, and `IsActive`; `notFound` means none
matched, including when the customer's privacy settings hide the address.
`HasMore` reports that the text match listed more customers than were compared.
A 404 means the desk is unknown or invisible and selects `providerRejected`.

### searchTickets

Searches one desk with Jira's enhanced search, `POST /rest/api/3/search/jql`.
`ProjectKey` is required; `StatusCategories`, `StatusNames`, `Labels`,
`ReporterAccountIDs`, `SummaryPhrase`, and `UpdatedSinceDate` (`YYYY-MM-DD`, in
the agent's Jira time zone) each add one escaped clause, so no caller text can
change the query. `BuildTicketSearchJQL` returns the query for review, such as
`project = "ITH" AND statusCategory in (2, 4) AND reporter in ("…") AND summary ~ "\"Laptop will not boot\"" ORDER BY created DESC`.
`PageSize` is 1 to 100 (zero requests 50); continue with `NextPageToken`. Search
is eventually consistent, so never use it to decide whether a create happened.

The request API's own list, `GET /rest/servicedeskapi/request`, is not used: by
default it returns only requests the connected account owns, participates in,
or sees through its organizations, not a desk's queue, and its `ALL_REQUESTS`
filter is deprecated.

### getTicket

Reads three resources and selects `found` only when all three succeed:

1. `GET /rest/api/3/issue/{key}` for the standard fields and the description,
   whose Atlassian Document Format is reduced to plain text (at most 32767
   characters, flagged `IsDescriptionTruncated`);
2. `GET /rest/servicedeskapi/request/{key}/comment` for the first
   `CommentLimit` comments (1 to 50, default 10), public and internal, with
   `HasMoreComments`;
3. `GET /rest/servicedeskapi/request/{key}/sla` for the SLAs.

A 404 from any of them, including a Jira issue that is not a service request,
selects `notFound`; a 403 from the SLA read, which only an agent of the desk may
make, selects `providerRejected`.

### createTicket

`POST /rest/servicedeskapi/request` with `serviceDeskId`, `requestTypeId`,
`requestFieldValues` (`summary`, an optional `description`, and up to 20
`AdditionalFieldValues` by Jira field ID for the request type's other fields),
and an optional `raiseOnBehalfOf` account ID. Jira Service Management stores the
description and comment bodies as text in which it renders wiki markup; the
connector sends them unchanged, so characters such as `*` and `_` can format
the text. The request API's ADF mode (`isAdfRequest`) is marked experimental
and is not used.

### updateTicket

Reads `GET /rest/api/3/issue/{key}?fields=labels,priority`, then sends
`PUT /rest/api/3/issue/{key}` with only the `add` and `remove` label operations
and the `priority` name that differ. When nothing differs it selects `updated`
with `WasAlreadyApplied` and writes nothing. The write is absolute, so every
unconfirmed outcome, including a 5xx or a lost response, is retried, and the
next attempt reads first. A conclusive 4xx selects `providerRejected` with the
rejected field IDs.

### transitionTicket

Moves the request through its Jira workflow exactly as the Jira connector's
`transitionIssue` does: it reads the status and available transitions in one
request, skips a transition the request has already made, selects
`transitionUnavailable` with up to 50 available transitions when none or
several match, and reads the request again after any ambiguous response. It
uses the platform transitions endpoint, which offers an agent every workflow
transition, rather than the request API's customer transitions. Set
`DestinationStatusName` whenever the destination is known.

### addComment

`POST /rest/servicedeskapi/request/{key}/comment` with `body` and an explicit
`public`. A 404 selects `notFound`.

## Duplicate safety

Jira Service Management documents no idempotency key, so a repeated create is a
second request and a repeated public reply is a second customer email. Dex
re-dispatches an async Step whose local attempt passes about seven seconds and
retries a Step after a lost Worker, so the writes take these positions:

- **createTicket** and **addComment** run with sync durability, so Dex never
  sends a second attempt while the first is in flight. Before sending, each
  records a Dex heartbeat checkpoint naming the Step's Call ID; a later attempt
  of the same Step execution that finds it, after a lost Worker or an Execute
  timeout, selects `uncertain` without sending. Only a 429 and a connection that
  never opened clear the checkpoint and retry; a 5xx, a 408, a lost or
  unreadable response, and an unusable 2xx select `uncertain`, and the connector
  never resends. A read-back marker was rejected because it would show in the
  customer's request or reply.
- **updateTicket** and **transitionTicket** keep async durability and read
  before they write, so a backup attempt finds the change already made.

Real Dex runs with a fake that answers after nine seconds guard each position:
`TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex`,
`TestSlowPublicReplyIsSentOnceUnderSyncDurabilityWithRealDex`,
`TestSlowLabelUpdateBackupAttemptWritesOnceWithRealDex` (two reads, one write),
and `TestSlowTransitionBackupAttemptMovesTheRequestOnceWithRealDex`. Do not
override `createTicket` or `addComment` to async. Recording the checkpoint
returns once the Worker writes it to its stream, before Dex stores it, so a
Worker lost after the request left but before Dex stored the checkpoint could
still send twice. A crash after the checkpoint but before the request left
reports `uncertain` for a request the provider never received. The
[`support-request`](examples/support-request) example parks an uncertain create
for an operator.

## Errors

Results, receipts, failures, and logs never contain the provider's
`errorMessage`, `errorMessages`, or `errors` text, or any token. A rejection
names the HTTP status, up to 20 field IDs from a Jira `errors` map, and the
request API's machine-readable `i18nErrorMessage.i18nKey` when it is a safe
token, such as
`Jira Service Management rejected the customer request with HTTP 400 (fields: customfield_10050) [sd.request.create.field.required]`;
`createTicket` also returns them as `RejectedFieldIDs` and `ProviderErrorKey`.

| Response | Reads, updateTicket, transitionTicket | createTicket, addComment |
| --- | --- | --- |
| 400, 401 after one refresh, 403, 409, 422, other 4xx | `providerRejected`; `transitionTicket` retries a 409 the read-back does not explain | `providerRejected` |
| 404 | `notFound` where declared | `notFound` (`addComment`), `providerRejected` (`createTicket`) |
| 429 | Retry after `Retry-After` | Retry after `Retry-After` |
| connection refused before sending | Retry | Retry |
| 408, 5xx, lost or unreadable response | Retry | `uncertain` |
| 3xx, oversized, malformed, or token-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input or credentials | `defect`, no request | `defect`, no request |

## Not in this release

### Triggers

Jira webhooks for OAuth 2.0 apps are registered dynamically with
`POST /rest/api/3/webhook` (classic scope `manage:jira-webhook`), carry a JQL
filter, and expire after 30 days unless `PUT /rest/api/3/webhook/refresh`
extends them. A Trigger would need a per-binding registration made from the
connection, a durable 30-day refresh schedule, and verification of Atlassian's
delivery authentication; `sdkgo/webhooktrigger` provides none of the first two,
and the delivery authentication of dynamic webhooks was not verified for this
release. Until then, poll with `searchTickets` and `UpdatedSinceDate`.

### Other gaps

- Customer creation (`POST /rest/servicedeskapi/customer` needs Jira
  administrator permission), request participants, approvals, organizations,
  attachments, and assignee changes.
- API-token (HTTP Basic) authentication, which Studio setup commands cannot
  send.
- A request type field picker; `AdditionalFieldValues` takes field IDs.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, RPC, persistence, duplicate-dispatch, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and UI:

```bash
go run ./cmd/connectorctl validate connectors/atlassian/jira-service-management/connector.yaml
go run ./cmd/connectorctl generate --check connectors/atlassian/jira-service-management/connector.yaml
(cd connectors/atlassian/jira-service-management/ui && npm ci && npm test && npm run build)
```

The deterministic fakes cover every branch above, JQL escaping and injection
attempts, ADF reading, comment visibility, SLA cycles, exact email matching and
the experimental-API header, the dispatch checkpoint and its clearing,
only-changed label and priority writes, one refresh after a 401, a second 401,
rate limits with `Retry-After`, redirects, oversized and malformed responses,
token reflection, site resolution, and the refresh driver's JSON grant,
rotation, 403 `invalid_grant`, outage, and scope checks.

No live Atlassian credentials were used. The following live behavior is
unverified: Dex Web's form-encoded authorization-code exchange against
Atlassian's token endpoint; whether the granted `scope` string lists all six
requested scopes, including `manage:servicedesk-customer` and `offline_access`;
the developer-console path **Permissions > Jira Service Management API**; real
response shapes of request create, comment, SLA, customer, enhanced search,
issue edit, and transition endpoints; whether `raiseOnBehalfOf` accepts only an
account ID; whether the customer list returns `emailAddress` for portal-only
customers and for desks with open access; the comment list's order; whether a
429 is always returned before a write is applied; the `X-Arequestid` header; the
`listServiceDesks` and `listRequestTypes` commands through Dex Web against a
real site; and the manual-ID hints that open `/rest/servicedeskapi/...` in a
signed-in browser.
