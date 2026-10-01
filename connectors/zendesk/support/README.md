# Zendesk Support Connector

The Zendesk Support Connector reads and writes Zendesk Support tickets from Dex
Flows. It exposes these operation-specific Dex Step factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `support.NewSearchTicketsStep` | Query | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetTicketStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewCreateTicketStep` | Mutation | `created` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewUpdateTicketStep` | Mutation | `updated` | `notFound`, `conflict`, `providerRejected`, `invalidResponse`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses async Execute durability, a 30-second Execute timeout, and a
six-attempt, five-minute retry window, which fits Zendesk's one-minute
`Retry-After` and stays far inside Zendesk's two-hour Idempotency-Key lifetime.

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Zendesk setup

A connection needs three values:

- **subdomain**, non-secret configuration: the `acme` in
  `https://acme.zendesk.com`. Every request goes to
  `https://{subdomain}.zendesk.com/api/v2`. A host-mapped domain does not work.
- **email**, a non-secret credential field: the agent or administrator the
  connector acts as. Tickets and comments it writes are authored by this user,
  and the user's role limits which tickets it can read and change.
- **api_token**, a secret credential field: a Zendesk API token.

An administrator turns on **Admin Center > Apps and integrations > APIs > API
configuration > Allow API token access**, then creates a token under **Apps and
integrations > APIs > API tokens > Add API token**. Zendesk shows the token
once. The connector authenticates with HTTP Basic as `{email}/token` and the
token. To revoke access, deactivate the token in Admin Center.

Zendesk is phasing API tokens out: tokens unused for 30 days are deleted from
July 28, 2026, and Zendesk plans to stop accepting API tokens on April 30,
2027. OAuth is the replacement, and it is blocked in this release; see
[OAuth](#oauth).

Credentials are reread before every provider call, so replacing a token in Dex
Web takes effect without a restart. The subdomain and response limit are
startup configuration.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "connectorId": "zendesk-support",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/zendesk/support",
  "moduleVersion": "v0.1.0",
  "provider": "zendesk",
  "connectionName": "zendesk-support-desk",
  "configuration": {"subdomain": "acme"},
  "credentials": {"email": "agent@acme.example.com", "api_token": "..."}
}
```

Load it with `localconfig.LoadFromEnvironment` and
`support.NewLocalConnection`, as
[`examples/customer-issue/main.go`](examples/customer-issue/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := support.NewLocalConnection(store, customerissue.ConnectionName, connectionOptions()...)
```

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
exactly `email` and `api_token` and rejects anything else without repeating
either value.

## Statuses, priorities, and types

The connector takes and returns Zendesk's own values and never maps them to
another vocabulary:

- status: `new`, `open`, `pending`, `hold`, `solved`, `closed`
  (`support.TicketStatuses()` lists them in Zendesk's order). With custom
  ticket statuses enabled, `status` is the status category and
  `customStatusId` is the custom status.
- priority: `low`, `normal`, `high`, `urgent`.
- type: `problem`, `incident`, `question`, `task`.

A Result passes through any value Zendesk adds later. Inputs are validated
against the documented values before any request.

## Operations

### searchTickets

`searchTickets` builds one Zendesk search query from typed filters and reads
one page of Zendesk's cursor-based export search, `GET /api/v2/search/export`
with `filter[type]=ticket`:

- `statuses`: any of the given statuses (`status:open status:pending`).
- `requesterEmail`: `requester:jane@example.com`.
- `tags`: any of the given lowercase tags (`tags:billing`).
- `updatedSince`: an RFC 3339 instant with an explicit offset
  (`updated>=2026-01-21T00:00:00Z`).
- `text`: up to 32 plain keywords that must all appear. A keyword containing
  a search operator (`: < > = " *`) or starting with `-` or `+` selects
  `defect`, so text cannot change the typed filters.

At least one filter is required. `BuildTicketSearchQuery` returns the query
for review. `pageSize` is 1 to 100, 25 by default, and `nextCursor` reads the
next page. The export endpoint is used instead of `/api/v2/search`, because
offset search stops at 1,000 results and can repeat results across pages.
Export search has its own consequences:

- results are ordered by creation time, and there is no sort parameter;
- a cursor expires one hour after its page was returned, and an expired cursor
  selects `providerRejected`;
- Zendesk rate-limits it to 100 requests per minute per account;
- Zendesk indexes new and changed tickets within a few minutes, so search is
  not a safe duplicate check immediately after a write.

Search pages omit ticket descriptions to stay small.

### getTicket

`getTicket` reads `GET /api/v2/tickets/{id}?include=users`, returns the
requester from the sideloaded users, and then reads the newest comments with
`GET /api/v2/tickets/{id}/comments?sort=-created_at&page[size]={limit}`.
`latestCommentLimit` is 1 to 20, 5 by default. Comments are returned newest
first, public replies and internal notes alike, with `isPublic` set, and
`hasOlderComments` reports that more exist. A missing ticket selects
`notFound`.

Every description and comment body is cut at 16 KiB (`support.MaxTextBytes`)
on a UTF-8 boundary and flagged `isDescriptionTruncated` or
`isBodyTruncated`, so Step state stays bounded.

### createTicket

`createTicket` sends `POST /api/v2/tickets` with the subject, the first comment
as a public reply or an internal note, an optional requester by email and name,
priority, type, tags, group, assignee, brand, and external ID.

Zendesk documents an `Idempotency-Key` header for ticket creation: a repeated
request with the same key and body returns the first response with
`x-idempotency-lookup: hit` instead of creating a second ticket, a repeated key
with a different body returns `400 IdempotentRequestError`, and keys expire
after two hours. The connector sends the Step's stable Call ID as the key, so
every attempt of one Step execution, including Dex's async backup attempt and
a replay after a Worker failure, sends the same key and the same body. The key
is also stored as `dex_idempotency_key` audit metadata. As a result:

- a lost connection, a timeout, a 408, a 409, a 429, or a 5xx is retried with
  the identical request, so the operation has no `uncertain` branch;
- `created` reports `wasIdempotentReplay: true` when Zendesk returned the
  cached response, and the Receipt carries `idempotencyLookup`;
- `IdempotentRequestError` selects `providerRejected` with a `CONFLICT`
  failure;
- a 2xx response that cannot be decoded selects `invalidResponse`, and the
  ticket may exist;
- a new Step execution is a new logical ticket. Set `externalId` to correlate
  tickets with an application record; Zendesk does not enforce its uniqueness.

### updateTicket

`updateTicket` changes status, priority, assignee, group, and tags, and can add
one comment as a public reply or an internal note. Zendesk documents no
idempotency key for updates, and a repeated comment is a duplicate that the
requester may be emailed, so every attempt runs this sequence:

1. Read the ticket and its exact `updated_at`.
2. When the request adds a comment or sets `expectedUpdatedAt`, read the newest
   audits with `GET /api/v2/tickets/{id}/audits?sort_order=desc`. An audit
   whose `metadata.custom.dex_idempotency_key` equals this Step's key means an
   earlier attempt already wrote the change, so the operation selects
   `updated` with `wasAlreadyApplied: true` and writes nothing.
3. When `expectedUpdatedAt` is set and the ticket changed after it, select
   `conflict` without writing.
4. Send only the fields that differ from the ticket as read. Tags are computed
   from the read tag list plus `addTags` minus `removeTags` and sent as the
   complete list. When nothing differs and there is no comment, select
   `updated` with `wasAlreadyApplied: true` without writing.
5. Write `PUT /api/v2/tickets/{id}` as a Zendesk safe update, with
   `safe_update: true`, `updated_stamp` set to the `updated_at` read in step 1,
   and the Step's key as `dex_idempotency_key` audit metadata.

A concurrent change, including a second dispatch of the same Step, makes
Zendesk reject the stale write with `409 UpdateConflict`, and the retried
attempt starts again at step 1 and finds its own marker. A closed ticket is
`providerRejected`; a missing ticket is `notFound`. This release cannot clear
an assignee or group.

## Errors

A non-2xx response never exposes Zendesk's `description` or detail message
text. A Failure repeats only Zendesk's machine-readable `error` code and up to
five `details` field names with their `type` or `error` token, such as
`Zendesk rejected the request (HTTP 422) [RecordInvalid; details: status=invalid]`.
A token that contains the API token is dropped.

| Response | Result |
| --- | --- |
| 400, 422 | `providerRejected`, `VALIDATION` (`CONFLICT` for `IdempotentRequestError`) |
| 401 | `providerRejected`, `AUTHENTICATION` |
| 403 | `providerRejected`, `AUTHORIZATION` |
| 404, 410 | `notFound` where declared, otherwise `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 408, 409, 5xx, transport failure | Retry, after `Retry-After` when present |
| 429 | Retry after `Retry-After` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` |
| invalid input or connection credentials | `defect`, with no request |

## Not in this release

### OAuth

Zendesk's OAuth authorization and token endpoints are per account:
`https://{subdomain}.zendesk.com/oauth/authorizations/new` and
`https://{subdomain}.zendesk.com/oauth/tokens`. The connector cannot declare
an OAuth method yet:

- the manifest schema requires static absolute HTTPS `authorizationEndpoint`
  and `tokenEndpoint` values and has no way to build them from the
  `subdomain` configuration field;
- Dex Web `cli-v1.1.0` sends the authorization-code exchange form-encoded and
  accepts only HTTP 200, while Zendesk documents JSON token requests and
  answers `201 Created`.

The refresh side is ready in the SDK: `sdkgo/oauthtoken` supports
`UsesJSONRequestBody`, `ClientSecretPost`, rotating refresh tokens, and
`AcceptsMissingExpiresIn` for Zendesk clients created before April 30, 2026,
whose tokens have no `expires_in`. Two SDK gaps remain for that method:
`AdditionalRequestParameters` are encoded as JSON strings, but Zendesk
documents `expires_in` as an integer; and `localconfig` requires a refresh
result with a future expiry, so a token Zendesk issues without a lifetime needs
a synthetic one.

### Studio pickers

A group or brand picker would list `GET /api/v2/groups` or
`GET /api/v2/brands`, but Studio setup commands declare a fixed HTTPS host and
support only bearer or raw-header credentials. Zendesk's host is per account,
and an API token needs HTTP Basic. Group, assignee, and brand IDs are therefore
operation input: find a group ID in Admin Center > People > Team > Groups, where
it is the number at the end of the group's URL.

### Triggers

Ticket events need Zendesk webhooks, which wait for the shared webhook source
design. Until then, poll with `searchTickets` and `updatedSince`.

## Example

[`examples/customer-issue`](examples/customer-issue) is a runnable Dex Web
**Start Flow** example that uses all four operations: it searches the
customer's unsolved tickets about an issue, reads the newest one to confirm it
still belongs to the customer, then adds one internal follow-up note, or opens
a ticket when there is none.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the pinned Dex development server running, the example owns its real
Worker, retry, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/zendesk/support/connector.yaml
go run ./cmd/connectorctl generate --check connectors/zendesk/support/connector.yaml
```

The provider fakes cover Basic authentication, query building and operator
rejection, export pagination, requester sideloads and newest-first comments,
the Idempotency-Key and its replay, safe updates and audit markers,
`expectedUpdatedAt` conflicts, Zendesk error codes and detail tokens without
message text, `Retry-After`, redirects, oversized, malformed, and
credential-reflecting responses, and invalid credentials.

No live Zendesk account was used. The following live behavior is unverified:
the export search response shape, ordering, and cursor expiry error; whether
`requester:` matches an email exactly; comment `sort=-created_at` and audit
`sort_order=desc` ordering; Zendesk's handling of two concurrent requests with
one Idempotency-Key; whether a 5xx response is cached under a key; the
`X-Idempotency-Lookup` header casing; safe-update comparison precision; audit
metadata storage for API-token requests; rate-limit headers; and the Admin
Center paths in the setup guidance.
