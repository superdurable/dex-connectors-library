# Zendesk Support Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/zendesk/support/go@v0.25.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/zendesk/support`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Zendesk; no live Zendesk account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Zendesk Support Connector reads and writes Zendesk Support tickets from Dex
Flows. It exposes these operation-specific Dex Step factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `support.NewSearchTicketsStep` | Query | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetTicketStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewCreateTicketStep` | Mutation | `created` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewUpdateTicketStep` | Mutation | `updated` | `notFound`, `conflict`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewListTicketsStep` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewDeleteTicketStep` | Mutation | `deleted` | `notFound`, `providerRejected`, `defect` |
| `support.NewRecoverSuspendedTicketStep` | Mutation | `recovered` | `notFound`, `providerRejected`, `defect`, `uncertain` |
| `support.NewListTicketFieldsStep` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetTicketFieldStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewListUsersStep` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetUserStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewSearchUsersStep` | Query | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewCreateUserStep` | Mutation | `created` | `providerRejected`, `defect`, `uncertain` |
| `support.NewUpdateUserStep` | Mutation | `updated` | `notFound`, `providerRejected`, `defect` |
| `support.NewDeleteUserStep` | Mutation | `deleted` | `notFound`, `providerRejected`, `defect` |
| `support.NewListOrganizationsStep` | Query | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewCountOrganizationsStep` | Query | `counted` | `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetOrganizationStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetOrganizationRelatedStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewListUserOrganizationsStep` | Query | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewGetUserRelatedStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `support.NewCreateOrganizationStep` | Mutation | `created` | `providerRejected`, `defect`, `uncertain` |
| `support.NewUpdateOrganizationStep` | Mutation | `updated` | `notFound`, `providerRejected`, `defect` |
| `support.NewDeleteOrganizationStep` | Mutation | `deleted` | `notFound`, `providerRejected`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses async Execute durability and a 30-second Execute timeout. Reads and
`createTicket` use the one-hour default retry budget, within Zendesk's two-hour
Idempotency-Key lifetime. `createTicket` repeats an unknown outcome safely
(`repeatOnUnknown: safe`), because every attempt sends the same Idempotency-Key.
`updateTicket` keeps six attempts within five minutes because comment
reconciliation scans only the latest audit page, and uses the default
`limited` tier with three sends.

It also exposes one Trigger:

- `ticketChanged` delivers each ticket created, updated, or deleted in the
  account, read through Zendesk's cursor-based incremental ticket export. It
  needs an administrator connection. See
  [ticketChanged Trigger](#ticketchanged-trigger).

This release has no Studio pickers and no webhook Triggers; see
[Not in this release](#not-in-this-release).

## Zendesk setup

A connection needs three values:

- **subdomain**, non-secret configuration: the `acme` in
  `https://acme.zendesk.com`. Every request goes to
  `https://{subdomain}.zendesk.com/api/v2`. A host-mapped domain does not work.
- **email**, a non-secret credential field: the agent or administrator the
  connector acts as. Tickets and comments it writes are authored by this user,
  and the user's role limits which tickets it can read and change. The
  `ticketChanged` Trigger reads the incremental ticket export, which Zendesk
  allows only for administrators, so a connection used by that Trigger names an
  administrator.
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
Web takes effect without a restart. The subdomain, response limit, and
`pollInterval` (the delay between `ticketChanged` polls, at least `5s`) are
startup configuration.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/customer-issue/main.go`](go/examples/customer-issue/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := support.NewProjectConnection(project, customerissue.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `email` and `api_token`; the connector
resolves it for every call and rejects anything else without repeating either
value.

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

`listTickets` reads one cursor page of up to 100 non-archived tickets visible to the connection's agent. Pass the returned `nextCursor` to read another page; the first page is used when it is blank. For a complete account export, use the incremental export API instead.

`listTicketFields` reads one cursor page of up to 100 system and custom field definitions; `getTicketField` reads one by positive ID. They return field ID, title, type, active, and required status. Provider field descriptions and other configuration are excluded from Flow results.

`listUsers` reads up to 100 users per cursor page. `getUser` reads one by ID, and `searchUsers` uses Zendesk's offset search with a nonempty query and at most 10,000 matches. User responses include selected profile fields and omit notes, custom fields, and authentication details.

`createUser` creates an end user with a required, account-unique `externalId` from the application. After an unknown result it reads users by that exact ID and confirms the name and email before resending, up to three sends. `updateUser` replaces supplied name, phone, tags, or suspension state; omitted fields are left alone. `deleteUser` removes a user and Zendesk does not offer recovery. These writes require a Zendesk administrator or an agent whose custom role can manage users. Updating a primary email address requires Zendesk's identity API and is outside `updateUser`.

`listOrganizations` and `listUserOrganizations` read up to 100 organizations per cursor page. `countOrganizations` returns Zendesk's approximate count; large accounts can see a count that lags by a day. `getOrganization` reads one by ID. `getOrganizationRelated` returns ticket and user counts, and `getUserRelated` returns assigned, requested, collaborated ticket, and organization subscription counts. Returned organization fields omit free-form notes and custom fields.

`createOrganization` requires an application-unique `externalId` so an unknown create can be checked with Zendesk's exact external-ID search before a bounded resend. `updateOrganization` replaces only supplied name, domains, tags, group, or sharing flags. `deleteOrganization` removes one by ID. Organization writes require a Zendesk administrator or an agent with organization-management permission.

`deleteTicket` soft-deletes one ticket by ID. Zendesk requires an administrator or an agent with deletion permission. `recoverSuspendedTicket` converts one suspended ticket to a regular ticket; Zendesk changes its requester to the authenticated agent. Recovery has a three-send cap and selects `uncertain` after an unconfirmed final attempt. Operators can inspect the suspended-ticket queue and the recovered ticket before manually retrying.

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
every attempt of one Step execution, including a retry after a Worker failure,
sends the same key and the same body. The key
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

## ticketChanged Trigger

`ticketChanged` polls Zendesk's
[cursor-based incremental ticket export](https://developer.zendesk.com/api-reference/ticketing/ticket-management/incremental_exports/)
(`GET /api/v2/incremental/tickets/cursor`) and delivers one
`support.TicketChangedEvent` per changed ticket. It is built on the Connector
SDK's durable poll Trigger kit
([`sdkgo/pollingtrigger`](../../../sdkgo/README.md#polling-triggers), sdkgo v0.38.0).

- **Administrator connection.** Zendesk allows the incremental export only for
  administrators. With an agent's email and token, every poll answers 403; the
  binding logs a WARN `poll failed; retrying` record that names the
  requirement and backs off. No new scope is involved: API tokens carry the
  role of the user they authenticate.
- **Baseline.** The first poll of a binding starts the export with
  `start_time` five minutes ago, so a Worker clock up to four minutes ahead
  of Zendesk's still sends a `start_time` Zendesk accepts, drains it to `end_of_stream` (at most 100
  pages), and keeps only the final `after_cursor`. Existing tickets are never
  delivered.
- **Changes.** Each later poll sends `cursor=<after_cursor>` with
  `per_page=100` and follows `after_cursor` while `end_of_stream` is false.
  The export reports each ticket once per poll, at its latest state, so
  several changes made between two polls arrive as one event. `GeneratedAt`
  is Zendesk's `generated_timestamp`, which also moves for system updates.
- **Change kind.** Zendesk reports no change type, so `ChangeKind` is derived:
  `deleted` is a ticket with status `deleted`; `created` means `updated_at` is
  within one second of `created_at`; every other change is `updated`.
- **Deletions.** By default the Trigger sends `exclude_deleted=true`. A
  binding whose saved configuration is `{"includeDeleted": true}` also
  receives deletions as
  `deleted` events, which carry status `support.TicketStatusDeleted`.
- **Cursor reset.** The cursor also stores the newest `generated_timestamp`
  delivered. When Zendesk answers a cursor request with 400 or 422, or the
  binding's `includeDeleted` changed, the next request restarts the export
  with `start_time` at that time (never less than 61 seconds ago), so no
  change is skipped; a restart within the last minute may repeat it, under the
  same event IDs. Changes that share one `generated_timestamp` second across
  a page boundary are all refetched, because the stored time moves past a
  second only at `end_of_stream`, when Zendesk has served all of it. A
  restart Zendesk also rejects fails the poll and backs off, keeping the
  cursor. Only a cursor that cannot be decoded records a new baseline and
  logs a WARN `poll cursor invalid; reseeding` record; changes in that gap
  are not delivered.
- **Rate limits.** Zendesk allows 10 incremental export requests per minute
  per account (30 with the High Volume API add-on). Every binding of one
  client shares a limiter that starts at most that many requests per minute;
  `support.WithIncrementalExportRequestsPerMinute(30)` raises it for a High
  Volume account. A 429 pauses every binding of the connection for
  `Retry-After`, or backs off exponentially without it. Other failures back
  off per binding, up to 15 minutes.
- **Page size.** A page larger than `maxResponseBytes` is requested again with
  half the `per_page`, down to 10 tickets, for the rest of the process; a page
  of 10 tickets that is still too large fails the poll until
  `maxResponseBytes` is raised. Descriptions are cut at `support.MaxTextBytes`.
- **Event IDs.** Each event ID hashes the ticket ID, `generated_timestamp`,
  `updated_at`, and status, so a refetch after a crash or a cursor restart
  yields the same ID and `sdkgo.NewDexFlowTriggerTarget` starts one Flow per
  ticket version.

The binding has no configuration unit, because Zendesk has no Studio setup
bundle (see [Studio pickers](#studio-pickers)): the binding declares no
Trigger field, and the zero configuration watches created and updated tickets
in the whole account. `includeDeleted` is read from the binding's saved
project configuration when present. A Flow's accept function narrows deliveries, for
example by `Ticket.GroupID` or `Ticket.BrandID`.

`support.NewProjectTicketTriggerRunner` opens the connection from the project,
records each change in the binding's project Trigger inbox before the cursor
commits in its project poll cursor, and serves every binding of the
connection from one goroutine, as
[`examples/ticket-changed/main.go`](go/examples/ticket-changed/main.go) does:

```go
triggerRunner, err := support.NewProjectTicketTriggerRunner(project, ticketchanged.ConnectionName, support.ProjectTicketTriggerRunnerConfig{
	TicketChangedRoutes: []support.ProjectTicketChangedTriggerRoute{{
		BindingName: ticketchanged.TicketChangedTriggerBinding, Target: newTicketChangeTarget(client, flow, logger),
	}},
}, options...)
```

`support.NewTicketTriggerRunner` takes targets and cursor stores directly. The
generated `support.NewTicketChangedTrigger` keeps its cursor in memory, so it
records a new baseline on every start. `support.WithLogger` sends the runner's
records, which never carry ticket payloads, cursors, or credentials, to a
logger. The application's storage grant needs `GetObject` and `PutObject` on
`trigger-inboxes/` and `poll-cursors/`.

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
documents `expires_in` as an integer; and `projectconfig` requires a refresh
result with a future expiry, so a token Zendesk issues without a lifetime needs
a synthetic one.

### Studio pickers

A group or brand picker would list `GET /api/v2/groups` or
`GET /api/v2/brands`, but Studio setup commands declare a fixed HTTPS host and
support only bearer or raw-header credentials. Zendesk's host is per account,
and an API token needs HTTP Basic. Group, assignee, and brand IDs are therefore
operation input: find a group ID in Admin Center > People > Team > Groups, where
it is the number at the end of the group's URL.

### Webhook Triggers

Push delivery of ticket events needs Zendesk webhooks and a signing-secret
verifier, which wait for the shared webhook source design. `ticketChanged`
polls instead and needs no public endpoint.

## Example

[`examples/customer-issue`](go/examples/customer-issue) is a runnable Dex Web
**Start Flow** example that uses all four operations: it searches the
customer's unsolved tickets about an issue, reads the newest one to confirm it
still belongs to the customer, then adds one internal follow-up note, or opens
a ticket when there is none.

[`examples/ticket-changed`](go/examples/ticket-changed) starts one Flow per
`ticketChanged` delivery, which reads the ticket back with
`support.NewGetTicketStep` and records it with its latest comment.

## Mocks

The generated `supportmock` defaults describe Jane's existing double-charge
ticket: search returns two unsolved matches and a newer solved one, getTicket
reads the newer unsolved one, and updateTicket adds an internal follow-up. A scripted empty search can use
createTicket's default, which opens ticket 35437. Named cases cover missing
tickets, stale updates, and permission refusals. The example uses the mock
connection with its real Flow:

```go
mock := supportmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

This snippet comes from
[`workflow_mock_integration_test.go`](go/examples/customer-issue/flow/workflow_mock_integration_test.go).

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
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
credential-reflecting responses, and invalid credentials. The `ticketChanged`
fake serves the cursor-based incremental export with per-epoch cursors, 422 for
a rejected cursor, 429 with `Retry-After`, 403 for an agent, and `per_page`
caps; the example's integration tests run it through a real Dex Worker and
Client.

No live Zendesk account was used. The following live behavior is unverified:
the export search response shape, ordering, and cursor expiry error; the
incremental ticket export's status code for an invalid cursor (the Trigger
assumes 400 or 422), its `per_page` handling, its 403 for an agent, the shape
of a deleted ticket, and its rate-limit headers; whether
`requester:` matches an email exactly; comment `sort=-created_at` and audit
`sort_order=desc` ordering; Zendesk's handling of two concurrent requests with
one Idempotency-Key; whether a 5xx response is cached under a key; the
`X-Idempotency-Lookup` header casing; safe-update comparison precision; audit
metadata storage for API-token requests; rate-limit headers; and the Admin
Center paths in the setup guidance.

## Python package

The independent `dex-connector-zendesk-support` Python `v0.2.0` package implements all four ticket operations and, from 0.2.0, the [`ticketChanged`](#ticketchanged-trigger) poll Trigger at parity with Go on the Python Connector SDK's poll Trigger kit (`dex-connectors-sdk` 0.12.0). Its cursor documents match Go's byte for byte, so Go and Python replicas can share one binding, and each ticket version keeps Go's event ID. Its [README](python/README.md), [read-ticket example](python/examples/read_ticket/README.md), and [ticket change example](python/examples/ticket_changed/README.md) show the static connection and Worker path. Go-recorded provider conformance scenarios cover Python request and outcome parity, Go-recorded export fetches cover the Trigger's requests, cursors, and events, and both examples run against a real local Dex Worker with a provider stand-in. Live Zendesk authorization and provider behavior remain unverified.

## TypeScript package

`@superdurable/connector-zendesk-support@0.2.0` implements all four ticket operations with generated operation-specific Step factories and, from 0.2.0, the [`ticketChanged`](#ticketchanged-trigger) poll Trigger at parity with Go on `@superdurable/connectors-sdk` 0.12.0. Its cursor documents match Go's byte for byte, so Go and TypeScript replicas can share one binding, and each ticket version keeps Go's event ID. See the runnable [TypeScript customer-issue Flow](typescript/examples/customer-issue/flow.ts) and [ticket-changed Flow](typescript/examples/ticket-changed/README.md). The TypeScript conformance suite replays Go-recorded provider requests and outcomes, Go-recorded export fetches cover the Trigger's requests, cursors, and events, and both examples run through a real Dex Worker and Client against provider stand-ins. Live Zendesk authorization and API behavior are unverified.
