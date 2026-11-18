# Gorgias Connector

The TypeScript package `@superdurable/connector-gorgias@0.2.1` implements all
six operations and the `ticketChanged` poll Trigger. See its
[package guide](typescript/README.md), its
[runnable ticket read](typescript/examples/read-ticket/README.md), and its
[ticket-changed example](typescript/examples/ticket-changed/README.md). Its provider
replay and Dex integration use a local stand-in; live Gorgias behavior is
unverified.

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/gorgias/go@v0.24.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/gorgias`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Gorgias; no live Gorgias account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Gorgias Connector reads and writes Gorgias helpdesk tickets from Dex Flows
through the Gorgias REST API. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `gorgias.NewSearchTicketsStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `gorgias.NewGetTicketStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `gorgias.NewFindCustomerByEmailStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `gorgias.NewCreateTicketStep` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `gorgias.NewUpdateTicketStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `gorgias.NewAddNoteStep` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Operation names, branch names, and record shapes follow the sibling desks.
`searchTickets`, `getTicket`, `createTicket`, `updateTicket`, and `addNote`
(with `isPublicReply`) carry the same names and branches as the
[Freshdesk](../freshworks/freshdesk/README.md) connector, `searchTickets` takes
a `nextCursor` like [Zendesk Support](../zendesk/support/README.md), and
`findCustomerByEmail` matches [Help Scout](../helpscout/README.md). A process
that swaps desks changes its connection and its status and priority values,
not its branches.

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Reads use a
30-second Execute timeout, `updateTicket` 45 seconds for its up to five
requests, and every operation retries within at least five minutes, which fits
Gorgias's 20-second rate-limit window and its `Retry-After` header.
`createTicket` and `addNote` use sync durability because Gorgias has no
idempotency key; see [Duplicate safety](#duplicate-safety).

It also exposes one Go poll Trigger, [`ticketChanged`](#ticketchanged-trigger),
which starts a Flow for every ticket event in the account's event log, such as
`ticket-created`, `ticket-updated`, or `ticket-closed`. The Python package
implements it from `v0.2.0`, and the TypeScript package from `v0.2.0`. This
release has no webhook Trigger,
no Studio pickers, and no OAuth method; see
[Not in this release](#not-in-this-release).

## Gorgias setup

A connection needs three values:

- **domain**, non-secret configuration: the `acme` in
  `https://acme.gorgias.com`. Every request goes to
  `https://{domain}.gorgias.com/api`. The connector accepts only lowercase
  letters, digits, and hyphens, so a scheme, a `.gorgias.com` suffix, a path,
  or another host is rejected before any request.
- **email**, a non-secret credential field: the Username of the Gorgias user
  the connector acts as. Internal notes and replies are authored by that user,
  and the user's role limits what the connector can read and change.
- **api_key**, a secret credential field: that user's API key.

The account owner or an admin signs in to `https://{domain}.gorgias.com`
from the [Gorgias login page](https://www.gorgias.com/login), selects the Settings icon at the bottom left, then **Account > REST API**, and
under **Password (API Key)** selects **Create API Key**, as Gorgias's
[REST API article](https://docs.gorgias.com/en-US/rest-api-208286) describes.
The page also shows the Base API URL and the Username. Gorgias shows the key
once; resetting it on the same page invalidates the old key immediately. The
connector authenticates with HTTP Basic, the email as the user name and the
key as the password, as Gorgias's API reference specifies.

Credentials are reread before every provider call, so replacing the key in Dex
Web takes effect without a restart. The domain, response limit, and
`pollInterval` are startup configuration.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/triage-issue/main.go`](go/examples/triage-issue/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := gorgias.NewProjectConnection(project, triageissue.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `email` and `api_key`; the connector
resolves them for every call and rejects a display-name address, a colon in
the email, or a key with spaces, without repeating either value.

## Statuses and priorities

The connector takes and returns Gorgias's own strings and never maps them to
another vocabulary. Gorgias has two statuses and four priorities:

| Gorgias status | Go constant | Zendesk equivalent | Freshdesk equivalent |
| --- | --- | --- | --- |
| `open` | `TicketStatusOpen` | `new`, `open`, `pending`, `hold` | 2, 3, 6, 7 |
| `closed` | `TicketStatusClosed` | `solved`, `closed` | 4, 5 |

| Gorgias priority | Go constant | Zendesk equivalent | Freshdesk equivalent |
| --- | --- | --- | --- |
| `low` | `TicketPriorityLow` | `low` | 1 |
| `normal` | `TicketPriorityNormal` | `normal` | 2 |
| `high` | `TicketPriorityHigh` | `high` | 3 |
| `critical` | `TicketPriorityCritical` | `urgent` | 4 |

The equivalence columns are guidance for a process that maps desks in the
open; the connector never applies them. A snoozed Gorgias ticket stays `open`
and carries `snoozedUntil`, the time Gorgias reopens it, which is the nearest
Gorgias has to a pending state. Inputs accept only the values above
(`gorgias.TicketStatuses()`, `gorgias.TicketPriorities()`), so a sibling
desk's value such as `solved`, `urgent`, or `4` selects `defect` before any
request. Results pass through whatever Gorgias returns.

Gorgias's ticket API documents no SLA due times, so `Ticket` carries the
timers it does document: `lastReceivedMessageAt`, `lastMessageAt`,
`closedAt`, `snoozedUntil`, and `trashedAt`. Tag names are case sensitive in
Gorgias, and the connector compares them exactly.

Gorgias's documented timestamps carry no offset, such as
`2019-07-05T14:42:00.384938`; the connector reads them as UTC and also
accepts an explicit offset.

## Operations

### searchTickets

`searchTickets` reads one page of `GET /api/tickets` with
`order_by=updated_datetime:desc` and `trashed=false`. Gorgias filters that
list only by customer, view, external ID, and ticket IDs, so the inputs split
in two:

- narrowed by Gorgias: `requesterEmail`, which the connector first resolves
  with `GET /api/customers?email=...` to the one customer whose primary email
  it is (Gorgias rejects a second customer with the same primary email), or
  `requesterId` directly; and `viewId`, the ID of a Gorgias view whose filters
  then apply, such as one an administrator built for a status and tag. With a
  view, Gorgias orders the page by the view's sort;
- filtered by the connector on each page: `statuses`, `priorities`, `tags`
  (any of each), and `updatedSince`, an RFC 3339 instant with an explicit
  offset. Without a view, the first ticket older than `updatedSince` also
  clears `nextCursor`, because every later ticket is older.

`pageSize` is 1 to 100 tickets, 30 by default, and `cursor` is a previous
page's `nextCursor`, read with the same inputs. Because the connector filters
after Gorgias pages, a page can hold fewer tickets than `pageSize`, even none,
while `nextCursor` is set; `listedCount` reports how many Gorgias listed. An
unknown `requesterEmail` returns an empty page without listing. Search
results omit messages.

### getTicket

`getTicket` reads `GET /api/tickets/{id}`, which embeds the customer and every
message, and returns the customer as `requester` and the latest
`latestMessageLimit` messages (1 to 20, 5 by default) newest first, internal
notes included, with `hasOlderMessages`. Each body is Gorgias's `body_text`,
or `stripped_text` when that is empty, cut at 16 KiB
(`gorgias.MaxTextBytes`) on a UTF-8 boundary and flagged `isBodyTruncated`.
HTML bodies are never carried. A ticket larger than `maxResponseBytes`
selects `invalidResponse`.

### findCustomerByEmail

`findCustomerByEmail` reads `GET /api/customers?email=...` and returns the
customers whose primary email address equals the input, ignoring letter case,
on `found`, or an empty list on `notFound`. Gorgias's `email` filter documents
no letter-case rule, so whether it lists a customer stored with different
capitalization depends on Gorgias. It accepts the documented list
envelope and, because the reference schema shows one, a bare array.

### createTicket

`createTicket` sends `POST /api/tickets` with the customer's email and name,
the subject (at most 998 characters), optional status, priority, tags,
assigned user, and assigned team, and one first message: the description as
`body_text`, HTML-escaped `body_html`, and `stripped_text`, sent by the
customer through the `api` channel. Gorgias adds a customer for an unknown
email. The `api` channel keeps Gorgias's auto-reply rules from emailing the
customer, while other rules, such as auto-tagging, still run. The ticket's
`external_id` is the Step's idempotency key, `dex-` followed by the Call ID;
see [Duplicate safety](#duplicate-safety).

### updateTicket

`updateTicket` reads `GET /api/tickets/{id}`, adds missing tags with
`POST /api/tickets/{id}/tags`, removes present ones with
`DELETE /api/tickets/{id}/tags`, and sends `PUT /api/tickets/{id}` with only
the status, priority, `assignee_user`, and `assignee_team` that differ. The
tag endpoints are set operations, so another agent's concurrent tag change is
never overwritten. When nothing differs it selects `updated` with
`wasAlreadyApplied: true` and writes nothing; after tag-only changes it reads
the ticket back. The tag writes go first, so a conclusive failure of a later
write, such as a `PUT` that names a deleted team, leaves the tag change
applied: the failure branch then carries the ticket as read again, or as
first read when that read fails. This release cannot unassign a user or team,
and Gorgias documents no conditional update, so a concurrent change to a
field this Step sets is overwritten.

### addNote

`addNote` sends `POST /api/tickets/{id}/messages`:

- by default, one internal note: channel `internal-note`, `public: false`,
  sent by the connection's user;
- with `isPublicReply`, one email reply. The connector first reads the
  ticket and answers the customer's newest email message: it sends to the
  address it came from, with its subject prefixed `Re:` and, when Gorgias
  reports one, the `integration_id` of the integration that received it.
  Gorgias sends an email only from an address one of its email integrations
  owns, so the reply comes from the address an earlier agent email through
  that same integration was sent from, or else from the email's only To or CC
  address other than the customer's own. When the email reached several
  addresses and no agent email identifies the helpdesk's, or the ticket has
  no customer email message, such as one `createTicket` opened through the
  `api` channel, the Step selects `providerRejected` without sending. Gorgias
  creates the message at once and emails it asynchronously, so the Result's
  `sentAt` is nil; a later `getTicket` shows `sentAt` or `failedAt`.

The message's `external_id` is the Step's idempotency key.

## Duplicate safety

Gorgias documents no idempotency key, so a repeated create or message is a
duplicate ticket, a second internal note, or a second email to the customer.
Dex can replay a Step after Worker loss or before a result is committed, so the operations take
these positions:

- **updateTicket** declares `repeatOnUnknown: safe`. Its writes set absolute
  field values computed from a fresh read and add or remove tags as sets, so a
  second dispatch sends the same values or finds them applied. It keeps async
  durability. An earlier attempt's write may have applied before any read of
  this attempt, so only a definite refusal (400, 401, 403, 404, 411, 413, 414,
  415, or 422) selects a branch; every other outcome, including a 5xx, 409,
  3xx, lost response, or invalid or oversized answer to the first read, a
  write, or the read after it, is retried. `invalidResponse` stays declared
  but is no longer selected. Revoked or unusable credentials select `defect`
  only on the first attempt; a later attempt returns Retry, because `defect`
  would hide a change an earlier attempt may have sent.
- **createTicket** and **addNote** are `limited` writes with the manifest send
  cap of three possible sends (`execution.maxSends`, default 3; an application
  may override it per Step). They run with sync durability under the SDK
  single-dispatch fence, which records a Dex heartbeat claim before each send
  and counts every possible send. Each send writes the Step's idempotency key
  to the ticket's or message's `external_id`, a field Gorgias leaves to the
  client.
- A 429, a 503, or a connection that failed before it opened proves Gorgias
  did not process the request. The fence records that, and the retry sends
  again without a lookup.
- A 2xx proves Gorgias accepted the write, and it is never sent again. When
  the 2xx body is invalid, oversized, credential-reflecting, or breaks while
  it is read, including when the fence write window ends after the 2xx
  headers arrived, the attempt reads the write back once: by the ID the body names
  for this Step's ticket or message, which the fence saves first, or else by
  `external_id`. A confirmed write selects `created` or `added`. A 429, 408,
  5xx, or refused connection on that read returns Retry only when the fence
  saved the ID; anything else selects `uncertain`.
- An attempt that finds a saved ID reads the ticket back by that ID, or the
  ticket's messages for that message ID, and never sends: a confirmed write
  selects `created` or `added` with `wasCreatedByEarlierAttempt: true`, a
  429, 408, 5xx, or refused connection returns Retry, and anything else
  selects `uncertain`.
- Every other outcome after a send is unknown: a 5xx other than 503, a 408,
  409, 3xx, or unlisted status, a refusal whose body exceeds
  `maxResponseBytes`, and a response lost before its status arrived. The
  attempt returns Retry, after Gorgias's `Retry-After` or two seconds,
  whichever is longer. The next attempt, like any attempt after a possible
  earlier send, including one after a resend that got a 429 or 503, first
  reads `GET /api/tickets?external_id=...` for a ticket, or the ticket's
  embedded messages for a note or reply:
  - the write is found, so it selects `created` or `added` with
    `wasCreatedByEarlierAttempt: true`;
  - the lookup gets a 429, 408, 5xx, or refused connection, so the attempt
    looks again after 15 seconds or Gorgias's `Retry-After`, capped at one
    minute, without sending;
  - Gorgias answers the lookup but shows no such write, or its answer is
    unreadable: below the cap the attempt sends again with the same
    `external_id`; at the cap it selects `uncertain` and never sends;
  - Gorgias refuses the lookup, for example with a 401, 403, or a 404 for the
    note's ticket: the attempt selects `uncertain` and never sends.

  The example Flow fails on the unwired `uncertain` branch; the application
  decides, for example by asking a person to check Gorgias.
- A definite refusal of the first send selects `providerRejected`, `notFound`,
  or `defect`. After a possible earlier send, a refusal, credentials that are
  revoked or unusable, or a public reply's ticket that cannot be read again
  selects `uncertain`, because the earlier send may have been applied. A
  possibly sent write never selects `defect` or `providerRejected`.
- Credentials that could not be read this time, such as an unavailable
  credential store, return Retry without recording a checkpoint after a
  possible earlier send. On a first send the fence records that nothing was
  sent; revoked or unusable credentials then select `defect`.
- An unreadable or untrusted fence checkpoint sends nothing and returns
  Retry. Markers written by `v0.24.0` and earlier (Go) or `v0.2.0` and earlier
  (Python and TypeScript) are connector-owned and untrusted by the SDK fence,
  so a write in flight during the upgrade retries without sending until its
  retry budget runs out and the Flow fails.

Keep these two Steps sync so results are committed promptly. A resend after
an empty lookup can still duplicate a write Gorgias applied but does not show
yet; the send cap bounds that to three tickets, notes, or replies per Step.

Because the connector owns `external_id` on the tickets it creates, store an
application correlation ID in a tag or in the Flow's own Attributes instead.

## Errors and rate limits

A non-2xx response never exposes Gorgias's message text. A Failure repeats
only a snake_case code in `error.msg`, such as `ticket_merged`, and up to five
field names under `error.data`, such as
`Gorgias rejected the request (HTTP 400) [fields: subject]`. A token that
contains the API key is dropped.

A write here means every `updateTicket` request, including its first read,
and the `createTicket` or `addNote` send.

| Response | Reads | Writes |
| --- | --- | --- |
| 400, 411, 413, 414, 415, 422 | `providerRejected`, `VALIDATION` for 400 | `providerRejected`; `uncertain` after a possible earlier create or message send; Retry, as for 409, when the body exceeds `maxResponseBytes` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same as 400 |
| 404 | `notFound` where declared, otherwise `providerRejected` | same as 400, with `notFound` where declared |
| 409, other 4xx | `providerRejected` | Retry; a create or message reads back by `external_id` first |
| 3xx, such as the 301 for a merged customer or ticket | `providerRejected`, `PROTOCOL`; redirects are never followed | Retry, as for 409 |
| 429, 503, connection refused before sending | Retry after `Retry-After` | Retry after `Retry-After`; a create or message sends again without a lookup |
| 408, other 5xx, lost or unreadable response | Retry, after `Retry-After` when present | Retry; a create or message reads back, resends below the cap, and selects `uncertain` at the cap |
| oversized, malformed, credential-reflecting, or broken 2xx | `invalidResponse` | Retry for `updateTicket`; a create or message is read back once and never resent |
| invalid input | `defect`, with no request | `defect`, with no request |
| connection credentials | `defect`, with no request | revoked or unusable: `defect`, or `uncertain` after a possible earlier create or message send, or Retry on a later `updateTicket` attempt; unreadable this time: Retry |

Gorgias limits API-key integrations to 40 requests per 20-second window, a
10-second window for Enterprise accounts, and answers 429 with `Retry-After`
in seconds. Each Receipt carries the Call ID, the ticket, message, or customer
ID, the `X-Gorgias-Account-Api-Call-Limit` value such as `10/40` as
`apiCallLimit` metadata, and an `X-Request-Id` header when Gorgias sends one.

## ticketChanged Trigger

`ticketChanged` polls Gorgias's
[list events](https://developers.gorgias.com/reference/list-events) endpoint,
`GET /api/events`, with `order_by=created_datetime:asc`,
`created_datetime[gte]`, `limit=100`, and one `types` value for each ticket
event type it reads (`gorgias.TicketEventTypes()`). It needs no Gorgias setup
and no permission beyond the connection's API key: it reads the events the
user's role can see. Each delivery is a `gorgias.TicketChangedEvent` with
`EventID`, `EventType`, `TicketID` (the event's `object_id`), `OccurredAt`,
`UserID` (zero for a rule or other automatic change), and `Context`. The event
names the ticket but not its fields, so read the ticket with `getTicket`, as
the example does.

- **Event types.** The Trigger reads the 22 `ticket-*` event types whose object
  is a ticket, from `ticket-created`, `ticket-updated`, `ticket-closed`, and
  `ticket-reopened` to `ticket-tags-added`, `ticket-merged`, and
  `ticket-deleted`. Message events such as `ticket-message-created` are not
  read: their object is a message, and Gorgias documents no ticket ID for it.
  One action in Gorgias can record several events, such as `ticket-updated`
  and `ticket-tags-added`, and each arrives as its own delivery.
- **Baseline.** The first poll of a binding records Gorgias's current time,
  from the response `Date` header, as the cursor and delivers no earlier event.
  The Worker's clock is used only when Gorgias sends no `Date` header, so a
  Worker clock running ahead of Gorgias's never skips events.
- **Cursor.** The cursor is the newest `created_datetime` read so far, at
  Gorgias's microsecond precision. Each poll starts 15 seconds earlier
  (`gorgias.TicketEventOverlap`), so an event whose write commits a little
  after its timestamp is still read, and the cursor remembers the IDs of the
  events inside that window, so none is delivered twice. The Trigger event ID
  is `gorgias-event-<id>`, the same on every refetch.
- **Pages.** A full page of 100 events continues at once from its last
  event's time, up to five pages per poll, and the rest waits for the next
  poll. Only a page of 100 events that all share one microsecond can skip
  events, because the next request starts one microsecond later.
- **Rate limits.** Gorgias allows an API-key integration 40 requests per
  20-second window (a 10-second window on Enterprise), shared by the
  connector's Steps and other integrations, and answers 429 with `Retry-After`
  in seconds, as its
  [limitations page](https://developers.gorgias.com/reference/limitations)
  documents. A 429 pauses every binding of the connection for `Retry-After`,
  or at least the poll interval. When the `X-Gorgias-Account-Api-Call-Limit`
  header shows three quarters of the window spent, such as `30/40`, a poll
  stops paging and leaves the rest for the next poll.
- **Failures.** A `created_datetime` filter never expires, so a 400, 401,
  403, 5xx, or invalid page backs off per binding and retries from the same
  position, and the events recorded meanwhile arrive once Gorgias answers
  again. Only an undecodable stored cursor records a new baseline and logs a
  WARN record; events between the last commit and the new baseline are not
  delivered.

The binding configuration, `gorgias.TicketChangedTriggerConfiguration`, has
one optional field. `eventTypes` limits deliveries, for example to
`["ticket-created"]` for new tickets or `["ticket-closed"]` for a satisfaction
follow-up. Empty delivers every type the Trigger reads. Skipped events still
move the cursor, so adding a type later never delivers events the binding
already passed. Studio setup commands cannot send the API key over HTTP Basic
to a per-account host (see [Studio pickers](#studio-pickers)), so the binding
has no picker units; the configuration is saved under the binding in the
project configuration.

`gorgias.NewProjectTicketTriggerRunner` loads each saved binding and records
every event in the binding's project Trigger inbox before the cursor commits,
so a restarted or replaced Worker neither misses nor repeats an event. From
[`examples/ticket-changed/main.go`](go/examples/ticket-changed/main.go):

```go
triggerRunner, err := gorgias.NewProjectTicketTriggerRunner(project, ticketchanged.ConnectionName, gorgias.ProjectTicketTriggerRunnerConfig{
	TicketChangedRoutes: []gorgias.ProjectTicketChangedTriggerRoute{{
		BindingName: ticketchanged.TicketChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, ticketchanged.AcceptEveryTicketChange, ticketchanged.ResolveFlowID,
			ticketchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, options...)
```

`gorgias.NewTicketTriggerRunner` takes explicit routes and cursor stores, and
`gorgias.WithLogger` sends the runner's records to an application logger. The
generated `gorgias.NewTicketChangedTrigger` keeps its cursor in memory, so it
records a new baseline on every start.

## Not in this release

### Triggers

Gorgias pushes events only through HTTP integrations: an administrator
configures a URL, method, headers, and a templated body that Gorgias sends on
`ticket-created`, `ticket-updated`, `ticket-message-created`, and similar
events. Gorgias documents no request signature, so a receiver could
authenticate a call only by a static header the integration sends in plain
configuration, and the templated body carries no event ID unless the
administrator adds one. `sdkgo/webhooktrigger` needs a verifiable request and
a stable event ID, so the connector declares no webhook Trigger and polls the
event log instead; see [ticketChanged Trigger](#ticketchanged-trigger).

### OAuth2

Gorgias requires OAuth2 for public apps, but its authorization and token
endpoints live on each account's host, `https://{domain}.gorgias.com/oauth/...`,
and an app must be created and approved in the Gorgias partner portal.
Manifest OAuth endpoints are static URLs, so a per-account host cannot be
declared. The connector offers the API-key method that Gorgias documents for
private apps.

### Studio pickers

A team, user, view, or tag picker would list `GET /api/teams`,
`GET /api/users`, `GET /api/views`, or `GET /api/tags`, but Studio setup
commands declare a fixed HTTPS host and support only bearer or raw-header
credentials. Gorgias's host is per account and an API key needs HTTP Basic, so
IDs stay operation input.

### Other limits

- Gorgias's ticket API documents no SLA due times.
- `updateTicket` cannot unassign a user or team.
- Public replies use the email channel only, routed from the customer's newest
  email message, and need a helpdesk address the connector can identify.

## Mocks

The generated `gorgiasmock` package models Ada's open billing ticket, a
three-page listing with a repeated ticket, an internal follow-up note, and a
separate new customer's refund ticket. The
[real-Dex example test](go/examples/triage-issue/flow/workflow_mock_integration_test.go)
uses that world to assert both the follow-up and new-ticket routes:

```go
mock := gorgiasmock.New(t, ConnectionName)
```

The mock package also supplies definite `notFound` and `providerRejected`
outcomes where each operation declares them. `findCustomerByEmail` reports
absence with an empty customer list, matching the real classifier. Script
other tickets, customers, and notes for different requests.

## Examples

[`examples/ticket-changed`](go/examples/ticket-changed) starts one Flow for
every ticket event and reads the ticket back with `getTicket`.

[`examples/triage-issue`](go/examples/triage-issue) is a runnable Dex Web
**Start Flow** example that uses all six operations: it finds the customer,
follows up on their open ticket about an issue or creates one, sets its
priority, adds one internal triage note, and optionally emails the customer
an acknowledgement.

The [Python package](python/README.md) implements all six operations and,
from `v0.2.0`, the `ticketChanged` poll Trigger at parity with Go: the same
event types, event IDs, overlap window, paging, and rate-limit rules, and poll
cursor documents that Go and Python replicas read and write byte for byte. Its
[ticket triage example](python/examples/triage_issue/README.md) binds the
static `gorgias` connection, reads a ticket and its latest messages, and its
[ticket changed example](python/examples/ticket_changed/README.md) starts one
Flow per ticket event on `gorgias-ticket-watcher`; both have real Dex Worker
and Client integration tests. The Python conformance suite replays Go-recorded
provider requests, branches, retries, and uncertainty. Live Gorgias behavior
for the Python package has not been verified.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, duplicate-dispatch, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/gorgias/connector.yaml
go run ./cmd/connectorctl generate --check connectors/gorgias/connector.yaml
```

The provider fakes cover Basic authentication, strict domain validation, the
customer lookup and page filters, cursors and the `updatedSince` stop,
newest-first messages and truncation, escaped HTML bodies, the SDK fence
claim, `canResend`, and saved-ID markers, the `external_id` lookup after a lost
response or Worker and before a resend below the send cap, `uncertain` at the
cap, a refusal after an unknown outcome, untrusted and previous-release
markers, repeated lookups and `Retry-After`, credential failures after a
possible send, reply routing from a known helpdesk
address, the ticket read again after a partly applied update, only-changed-field updates and set-based tag changes,
Gorgias error codes and field names without message text, `Retry-After`,
the event log baseline, overlap window, full-page scan, binding event-type
filter, rate-budget stop, 429 pause, 400 retry from the same position,
undecodable-cursor reseed,
redirects, offset-less timestamps, oversized, malformed, and
credential-reflecting responses, and invalid credentials.

No live Gorgias account was used. The following live behavior is
unverified: whether `GET /api/customers` returns the documented list envelope
or a bare array; whether `GET /api/tickets/{id}` returns `priority` and
`assignee_team`, which only the list and update schemas document; whether
`customer_id` and `view_id` combine; whether a ticket and a message created
with an `external_id` are immediately visible to `GET /api/tickets?external_id=`
and to the ticket's embedded messages; whether `POST` and `DELETE
/api/tickets/{id}/tags` are no-ops for present and absent tags; whether a
reply sent from the address the customer wrote to is accepted for every email
integration; whether Gorgias reports `integration_id` on received and sent
emails and accepts it on a reply; whether 429 responses are always returned before a write is
applied; the error body shape beyond `error.msg` and `error.data`; the
`X-Request-Id` header; the Settings > Account > REST API path in the setup
guidance; and, for `ticketChanged`, whether `GET /api/events` returns the
documented bare array, accepts `created_datetime[gte]` with a `Z` offset and
repeated `types` values, reports `object_type: Ticket` with the ticket ID as
`object_id` for every `ticket-*` type it reads, and how long Gorgias keeps
events.
