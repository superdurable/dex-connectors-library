# Zoho Desk Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Zoho Desk; no live Zoho Desk account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Zoho Desk Connector reads and writes Zoho Desk tickets from Dex Flows
through the Zoho Desk API v1, in the Zoho data center where the account lives.
It exposes these operation-specific Dex Step factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `desk.NewSearchTicketsStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `desk.NewGetTicketStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `desk.NewCreateTicketStep` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `desk.NewUpdateTicketStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `desk.NewAddCommentStep` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout and a retry window of five minutes, and keeps
all of its requests, including a token refresh, inside a 25-second deadline:
each Zoho Desk request times out after 10 seconds and each token request after
8, unless `WithHTTPClient` supplies a client with its own `Timeout`.
`createTicket` and `addComment` use sync durability because Zoho Desk has no
idempotency key; see [Duplicate safety](#duplicate-safety).

The connection form has one Studio unit, `organizationPicker`, for the
`orgId` field. There are no Triggers; see [Not in this release](#not-in-this-release).

## Data centers

Zoho keeps each account in one data center, and both the OAuth server and the
Zoho Desk API host belong to it. A manifest declares static OAuth endpoints,
so the connector declares one authorization method per data center. Dex Web
lists them under **Data center**; choose the one whose Zoho Desk address you
sign in to:

| Method ID | Data center | Zoho Accounts (OAuth) | Zoho Desk API |
| --- | --- | --- | --- |
| `zoho-us-oauth` (default) | United States | `https://accounts.zoho.com` | `https://desk.zoho.com/api/v1` |
| `zoho-eu-oauth` | European Union | `https://accounts.zoho.eu` | `https://desk.zoho.eu/api/v1` |
| `zoho-in-oauth` | India | `https://accounts.zoho.in` | `https://desk.zoho.in/api/v1` |
| `zoho-au-oauth` | Australia | `https://accounts.zoho.com.au` | `https://desk.zoho.com.au/api/v1` |
| `zoho-jp-oauth` | Japan | `https://accounts.zoho.jp` | `https://desk.zoho.jp/api/v1` |
| `zoho-ca-oauth` | Canada | `https://accounts.zohocloud.ca` | `https://desk.zohocloud.ca/api/v1` |
| `zoho-sa-oauth` | Saudi Arabia | `https://accounts.zoho.sa` | `https://desk.zoho.sa/api/v1` |
| `zoho-sg-oauth` | Singapore | `https://accounts.zoho.sg` | `https://desk.zoho.sg/api/v1` |
| `zoho-ae-oauth` | United Arab Emirates | `https://accounts.zoho.ae` | `https://desk.zoho.ae/api/v1` |

These are the data centers for which the Zoho Desk API documentation lists an
API host and Zoho Accounts lists a server in
`https://accounts.zoho.com/oauth/serverinfo`. China (`desk.zoho.com.cn`) is a
separately operated service that `serverinfo` does not list, and the Zoho Desk
documentation lists no United Kingdom host, so neither is offered.
`desk.DataCenters()` returns the same table.

Zoho's token response carries `api_domain`, but for Zoho it is the shared
`https://www.zohoapis.<domain>` host, which answers Zoho Desk paths with
`API endpoint not found`. The Zoho Desk host is fixed by the method instead,
so `api_domain` is neither mapped by Dex Web nor retained on refresh.

## Zoho setup

One Zoho API console client serves every Dex connection. In the API console of
the data center, such as `https://api-console.zoho.eu`:

1. Choose **GET STARTED** or **ADD CLIENT** and create a **Server-based
   Applications** client with a Client Name and Homepage URL.
2. Add the Redirect URI that Dex Web shows under **Authorized Redirect URIs**
   and choose **CREATE**. A client registered in another data center needs this
   data center turned on in its **Settings** tab; the client ID is shared, and
   each data center can have its own client secret.
3. Copy the **Client ID** and this data center's **Client Secret** from the
   **Client Secret** tab into Dex Web.
4. Choose **Connect** and sign in as the Zoho Desk agent the connector acts
   as. Tickets and comments it writes are authored by that agent, and the
   agent's profile limits what it can read and change. On the consent screen,
   accept every scope; if the account belongs to several Zoho Desk
   organizations, pick the one the connection uses.

Dex requests `access_type=offline` and `prompt=consent`, so Zoho returns a
refresh token, and these scopes:

| Scope | Used by |
| --- | --- |
| `Desk.tickets.READ` | `getTicket`, `searchTickets`, `updateTicket`'s read |
| `Desk.tickets.CREATE` | `createTicket`, including a new contact |
| `Desk.tickets.UPDATE` | `updateTicket`, `addComment` |
| `Desk.search.READ` | `searchTickets` |
| `Desk.basic.READ` | the organization picker's `GET /api/v1/organizations` |

PKCE is off, because Zoho documents none for server-based clients. To revoke
access, remove the connected app from the Zoho account or delete the client in
the API console; every operation then selects `providerRejected` with an
`AUTHENTICATION` failure until the connection is reauthorized.

### Choosing the organization

Every request sends the non-secret `orgId` header. Zoho binds a token to the
organization chosen at consent and rejects any other with
`OAUTH_ORG_MISMATCH`, which the connector reports as `providerRejected` with
an explanation. The `orgId` field is rendered by the `organizationPicker` unit
once the connection is **Ready**. The unit runs only the setup command of the
connection's own data center, such as `listOrganizationsEU`
(`GET https://desk.zoho.eu/api/v1/organizations`), so the access token never
reaches another data center's host. When Dex Web reports no data center or the
list cannot load, the unit accepts a typed `orgId`, which Zoho Desk shows under
**Setup > Developer Space > API**.

Dex Web renders a field's Studio unit only when the first save can leave it
blank, so the manifest marks `orgId` optional, but `desk.New` rejects a blank
or non-numeric `orgId`: the application refuses to start until the
organization is chosen. Studio commands send the stored access token, which
Dex Web does not refresh, so run the picker soon after **Connect** or
**Reconnect**.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "connectorId": "zoho-desk",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/zoho/desk",
  "moduleVersion": "v0.1.0",
  "provider": "zoho",
  "connectionName": "zoho-desk-helpdesk",
  "authMethodId": "zoho-eu-oauth",
  "configuration": {"orgId": "2389290"},
  "credentials": {"auth_method": "zoho-eu-oauth", "oauth_client_id": "...", "oauth_client_secret": "...", "access_token": "...", "refresh_token": "..."},
  "credentialExpiresAt": "2026-09-30T10:00:00Z"
}
```

Load it with `localconfig.LoadFromEnvironment` and `desk.NewLocalConnection`,
as [`examples/triage-issue/main.go`](examples/triage-issue/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := desk.NewLocalConnection(store, triageissue.ConnectionName, connectionOptions()...)
```

Credentials are reread, and refreshed when due, before every provider call;
`orgId` and `maxResponseBytes` are startup configuration.

## Token refresh

Zoho access tokens last one hour. `CredentialRefreshDriver` refreshes them
through `sdkgo/oauthtoken` at the method's
`{accounts server}/oauth/v2/token`, with a form body and the client secret in
the body. Zoho does not rotate refresh tokens, so the stored one is kept, and
the recorded expiry never exceeds the documented hour even if `expires_in`
says otherwise. Zoho answers grant errors with HTTP 200 and an `error` code;
`invalid_code` (a revoked refresh token), `invalid_client` (including a client
not enabled in this data center), and `invalid_client_secret` require
reauthorization, and every other failure, such as Zoho's `Access Denied`
throttle of ten tokens per ten minutes, is retried. Zoho's documented token
responses carry no `scope`, so the driver checks none, and a lost scope
surfaces as `SCOPE_MISMATCH` on the next request.

When Zoho Desk answers 401, the connector forces one coordinated refresh and
sends the request once more, because Zoho Desk rejects an expired or revoked
token before acting on it. A second 401 selects `providerRejected`.

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider and `DecodeResolvedCredentialsJSON`, which
accepts exactly `auth_method`, which selects the data center, and
`access_token`, and rejects refresh tokens and client secrets.

## Statuses, status types, and priorities

The connector takes and returns Zoho Desk's own names and never maps them to
another vocabulary:

- **Status** is the status name, including an organization's custom statuses.
  A new organization has `Open`, `On Hold`, `Escalated`, and `Closed`
  (`desk.DefaultTicketStatuses()`). The organization's list is the
  `allowedValues` of the `status` field in
  `GET /api/v1/organizationFields?module=tickets`.
- **Status type** is the category every status falls under: `Open`,
  `On Hold`, or `Closed` (`desk.TicketStatusTypes()`). `Escalated` is of type
  `Open`. Zoho Desk spells the types both `OPEN`/`ONHOLD`/`CLOSED` and
  `Open`/`On Hold`/`Closed`; Results report the latter, and pass any other
  value through. `Ticket.IsClosed()` checks the type, not the name.
- **Priority** is `High`, `Medium`, or `Low` by default
  (`desk.DefaultTicketPriorities()`), or a custom priority.

Inputs accept any name of 1 to 120 characters without surrounding spaces,
control characters, or `, * $ { }`, so a name cannot add a search value,
wildcard, or empty check. Zoho Desk rejects an unknown name as
`providerRejected`.

## Operations

### searchTickets

`searchTickets` reads one page of `GET /api/v1/tickets/search`, most recently
modified first (`sortBy=-modifiedTime`):

- `statuses`: any of these status names (`status=Open,Waiting for Customer`).
- `statusTypes`: any of these types (`status=${OPEN},${ONHOLD}`), so custom
  statuses match without listing them. Set `statuses` or `statusTypes`, not
  both.
- `priorities`: any of these priorities (`priority=High`).
- `contactEmail`: one bare address (`email=jane@example.com`). Zoho Desk's
  email filter also matches look-alike addresses, so the connector keeps only
  tickets whose email or contact email equals the address, ignoring case. A
  page can then hold fewer tickets than `limit`, even none, while `nextFrom`
  is set.
- `departmentId`: one department; blank searches every department the agent
  can see.
- `modifiedSince`: an RFC 3339 instant with an explicit offset, sent as
  `modifiedTimeRange={modifiedSince},{now}`.

At least one filter is required. `from` is 0 to 4999 and `limit` 1 to 100, 25
by default; a page that would pass result 5000, the depth Zoho Desk's search
serves, is shortened to end there, so every `nextFrom` can be read. `nextFrom`
is `from + limit` when the page was full and ended before result 5000, and zero
otherwise; `totalMatched` is Zoho Desk's `count`. A 204 is an empty page.
`BuildTicketSearchParameters` returns the parameters for review. Search pages
omit descriptions, and Zoho Desk indexes new and changed tickets with a delay,
so search is not a duplicate check immediately after a write.

### getTicket

`getTicket` reads `GET /api/v1/tickets/{id}?include=contacts`, then the newest
threads with `GET /api/v1/tickets/{id}/threads?from=0&limit={n}&sortBy=-sendDateTime`
and the newest comments with
`GET /api/v1/tickets/{id}/comments?from=0&limit={n}&sortBy=-commentedTime`.
`threadLimit` and `commentLimit` are 1 to 20, 5 by default. Threads carry Zoho
Desk's plain-text `summary`, not the full content, and neither threads nor
comments carry email addresses. `hasMoreThreads` and `hasMoreComments` compare
with the ticket's `threadCount` and `commentCount`. The description is the HTML
Zoho Desk stores (`descriptionHtml`); it, each summary, and each comment are
cut at 16 KiB (`desk.MaxTextBytes`) on a UTF-8 boundary and flagged. A missing
ticket selects `notFound`.

### createTicket

`createTicket` sends `POST /api/v1/tickets` with the subject, a plain-text
description, the department, exactly one of `contactId` or a contact by email
with optional names, and optional status, priority, and assignee. Zoho Desk
uses the contact with that email or adds one, and the connector also sets the
ticket's email to it, which `contactEmail` searches match. The description is
HTML-escaped with line breaks kept, so text is never interpreted as markup;
the escaped HTML must fit Zoho Desk's 65,535-character limit.

### updateTicket

`updateTicket` sets the status, priority, assignee, or department. It reads
`GET /api/v1/tickets/{id}`, then sends `PATCH /api/v1/tickets/{id}` with only
the fields that differ; status and priority names compare without regard to
letter case. When nothing differs, it selects `updated` with
`wasAlreadyApplied: true` and writes nothing. Moving the department resends the
assignee, if set, because Zoho Desk applies the new department's rules. A
blueprint that forbids the transition, or an assignee outside the department,
is `providerRejected`. `updated` returns the ticket as Zoho Desk answered the
write, including any change its automations made, without comparing it with
the request. When a department move is applied but its response is lost, the
retry reads the ticket again; an agent who cannot see the new department then
gets `notFound` or `providerRejected` for a move that happened. This release
cannot clear an assignee.

### addComment

`addComment` sends `POST /api/v1/tickets/{id}/comments` with
`contentType: plainText` and `isPublic` stated explicitly. A public comment is
shown to the customer in the help center; a private one only to agents. Zoho
Desk fixes visibility when the comment is added, and a response with another
visibility selects `uncertain`. Content is at most 32,000 characters.

## Duplicate safety

Zoho Desk documents no idempotency key, so a repeated create or comment is a
duplicate ticket or a second comment, which a customer sees when it is public.
Dex re-dispatches an async Step whose local attempt passes about seven
seconds, and retries a Step after a lost Worker, so the operations take these
positions:

- **updateTicket** is safe to repeat. Its write sets absolute values computed
  from a fresh read, so a second dispatch sends the same values or finds them
  applied. It keeps async durability, and every unconfirmed outcome, including
  a 5xx or a lost response, is retried.
- **createTicket** and **addComment** run with sync durability, so Dex never
  sends a second attempt while the first is in flight. Before sending, the
  operation records a Dex heartbeat checkpoint naming the Step's Call ID. A
  later attempt of the same Step execution that finds the checkpoint, after a
  lost Worker or an Execute timeout, selects `uncertain` without sending. The
  checkpoint is read before credentials, so a revoked authorization on that
  later attempt still reports `uncertain`, not "nothing was created".
- Only an outcome that proves Zoho Desk did not apply the request is retried:
  a 429, which Zoho Desk returns instead of processing; a DNS, connect, or TLS
  failure before any connection was obtained, traced with `httptrace`; the
  operation deadline passing before the request started; and a 401 followed
  by one refresh. The checkpoint is cleared first.
- A 5xx, a 408, a lost or unreadable response, and an invalid, oversized, or
  credential-reflecting 2xx select `uncertain`: the ticket or comment may
  exist, and the connector never resends it. The application decides, for
  example by asking a person to check Zoho Desk, as the example does.

Keep these two Steps sync. An application override to async durability with
`StepOptionsOverride` lets Dex dispatch a second request after seven seconds,
and the generated factories do not reject it. Dex can acknowledge a heartbeat
before it stores it, so a checkpoint lost before Dex stored it, or a crash
before the request left, can make an attempt report `uncertain` for a request
Zoho Desk never received; that direction is safe.

## Errors

A non-2xx response never exposes Zoho Desk's `message` or `errorMessage` text.
A Failure repeats only Zoho Desk's machine-readable `errorCode` and up to five
`errors` entries as `fieldName=errorType`, such as
`Zoho Desk rejected the request (HTTP 422) [INVALID_DATA; errors: /departmentId=invalid]`.
A body that contains the access token is dropped.

| Response | Reads and updateTicket | createTicket and addComment |
| --- | --- | --- |
| 400, 422 (`INVALID_DATA`, `UNPROCESSABLE_ENTITY`) | `providerRejected`, `VALIDATION` | same |
| 401 (`INVALID_OAUTH`) after one refresh | `providerRejected`, `AUTHENTICATION` | same |
| 403 (`OAUTH_ORG_MISMATCH`, `SCOPE_MISMATCH`, `FORBIDDEN`, `LICENSE_ACCESS_LIMITED`) | `providerRejected`, `AUTHORIZATION` | same |
| 404 (`URL_NOT_FOUND`) | `notFound` where declared, otherwise `providerRejected` | same |
| 405, 409, 413, 415, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 (`THRESHOLD_EXCEEDED`, `TOO_MANY_REQUESTS`) | Retry, after `Retry-After` when present | Retry, after `Retry-After` when present |
| DNS, connect, or TLS failure before a connection, or the deadline passed before sending | Retry | Retry |
| 408, 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| revoked authorization (`invalid_code` on refresh) | `providerRejected`, `AUTHENTICATION` | same |
| token refresh outage | Retry | Retry |
| invalid input, unknown data center, or unusable token | `defect`, with no request | `defect`, with no request |

Zoho Desk meters API credits per day and concurrent calls per edition. It sends
`Retry-After` only when the daily credits are exhausted, often hours ahead;
`Retry-After` is capped at one hour, and a delay that does not fit the
five-minute retry window fails the Step. The Receipt carries the Call ID, the
ticket or comment ID, and Zoho Desk's `X-Rate-Limit-Request-Weight-v3` and
`X-Rate-Limit-Remaining-v3` headers as `requestCredits` and
`remainingCredits` when present.

## Not in this release

### Triggers

Zoho Desk webhooks are not Triggers yet:

- A subscription is created through `POST /api/v1/webhooks`, which first sends
  a validation `GET` to the URL and then a validation `POST`, and fails unless
  one answers 200. `webhooktrigger` has no handshake hook and answers `GET`
  with 405.
- Deliveries carry an `X-ZDesk-JWT` RS256 token signed with keys from
  `https://desk.zoho.com/.well-known/jwks.json`. Its claims name the
  organization (`iss`) and webhook (`aud`) and expire minutes later, but they
  do not cover the request body, so the signature proves the sender, not the
  payload. Verifying it needs a JWKS fetch for each data center, and
  `webhooktrigger` verifiers have no key source.
- Free and Standard editions have no webhooks.

Until a Trigger can complete the handshake and bind the payload, poll with
`searchTickets` and `modifiedSince`.

### Other limits

- **Dex Web OAuth.** Use Dex CLI `cli-v1.4.0` or later. Earlier Dex Web
  releases require the token response's `scope` to list every requested scope,
  and Zoho's documented token responses have no `scope` field; since
  `cli-v1.4.0` (superdurable/dex#581) a response without `scope` grants the
  requested scopes, as RFC 6749 allows. Dex Web sends the manifest scopes
  joined by spaces, while Zoho documents comma-separated scopes; whether Zoho
  accepts spaces is not verified against a live Zoho account.
- **Conflict detection.** Zoho Desk documents no conditional update, so a
  concurrent change by another agent between `updateTicket`'s read and write is
  overwritten for the fields the Step sets.
- **Full thread content** needs `GET /api/v1/tickets/{id}/threads/{threadId}`,
  which no operation reads yet.

## Example

[`examples/triage-issue`](examples/triage-issue) is a runnable Dex Web
**Start Flow** example that uses all five operations: it finds the contact's
open or on-hold ticket in a department or creates one, sets its priority, and
adds one private triage comment.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, duplicate-dispatch, token-refresh, and transition
coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

The Studio bundle has its own tests:

```bash
npm ci --prefix ../../../sdk/react && npm run build --prefix ../../../sdk/react
cd ui && npm ci && npm test && npm run build
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/zoho/desk/connector.yaml
go run ./cmd/connectorctl generate --check connectors/zoho/desk/connector.yaml
```

The provider fakes cover the `Zoho-oauthtoken` and `orgId` headers, each data
center's own Zoho Desk and Zoho Accounts hosts, the one refresh and resend
after a 401, non-rotating refresh tokens and terminal refresh codes, search
parameter building and value rejection, the contact-email check, `from` and
`limit` paging and 204 pages, string IDs and counts, newest-first threads and
comments, escaped HTML descriptions, the dispatch checkpoint and its clearing,
only-changed-field updates, error codes and field tokens without message text,
`Retry-After`, redirects, and oversized, malformed, and credential-reflecting
responses.

No live Zoho account was used. The following live behavior is unverified:
whether Zoho accepts space-separated scopes at `/oauth/v2/auth`; whether `Desk.tickets.CREATE`
alone lets `createTicket` add a contact; whether `Desk.basic.READ` and Bearer
authorization serve `GET /api/v1/organizations` to the picker; whether
`${OPEN},${ONHOLD}` combine in one `status` value; whether the `email` filter
matches look-alike addresses as the connector assumes; how `modifiedTimeRange`
treats ranges longer than a month; whether `from` above 999, which the OpenAPI
file sets as its maximum, is accepted; whether empty thread and comment lists
answer 204; whether the comments list returns `plainText`; whether an
unchanged `PATCH` triggers workflows; whether every 429 is returned before a
write is applied; the credit headers; and the **Setup > Developer Space > API**
path to the organization ID.
