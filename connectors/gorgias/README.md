# Gorgias Connector

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

This release has no Triggers, no Studio pickers, and no OAuth method; see
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
Web takes effect without a restart. The domain and response limit are startup
configuration.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/triage-issue/main.go`](examples/triage-issue/main.go) does:

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
Dex re-dispatches an async Step whose local attempt passes about seven
seconds, and it retries a Step after a lost Worker, so the operations take
these positions:

- **updateTicket** is safe to repeat. Its writes set absolute field values
  computed from a fresh read and add or remove tags as sets, so a second
  dispatch sends the same values or finds them applied. It keeps async
  durability, and every unconfirmed outcome, including a 5xx or a lost
  response, is retried.
- **createTicket** and **addNote** run with sync durability, so Dex never sends
  a second attempt while the first is in flight. Before sending, the operation
  records a Dex heartbeat checkpoint naming the Step's Call ID and writes the
  Step's idempotency key to the ticket's or message's `external_id`, a field
  Gorgias leaves to the client.
- Only an outcome that proves Gorgias did not apply the request is retried
  with a resend: a 429, which Gorgias returns instead of performing the
  request, and a connection that failed before it opened. The checkpoint is
  cleared first, and the retry, like every later attempt that finds no
  checkpoint, first looks for the write by its `external_id`: it selects
  `created` or `added` when the write is there and sends only when it is not.
- A 5xx, a 408, a lost or unreadable response, and an invalid or oversized
  2xx are retried only to look for the write, after Gorgias's `Retry-After`
  or two seconds, whichever is longer. So is any attempt that finds the
  checkpoint, such as after a lost Worker or an Execute timeout. That attempt
  checks the checkpoint before it resolves credentials, reads
  `GET /api/tickets?external_id=...` for a ticket, or the ticket's embedded
  messages for a note or reply, and never sends:
  - the write is found, so it selects `created` or `added` with
    `wasCreatedByEarlierAttempt: true`;
  - Gorgias does not show the write yet, the lookup gets a 429, 408, 5xx,
    refused connection, or unreadable page, or the connection's credentials
    cannot be resolved, so the attempt keeps the checkpoint and looks again
    after 15 seconds or Gorgias's `Retry-After`, capped at one minute. The
    Step's fourth attempt that still cannot confirm the write, or a lookup
    Gorgias conclusively rejects, selects `uncertain`. Gorgias may still apply
    a request it was processing, so the connector never resends it, and the
    application decides, for example by asking a person to check Gorgias, as
    the example does. A possibly sent write never selects `defect`.

Keep these two Steps sync. An application override to async durability lets
Dex dispatch a second request after seven seconds, before the first is
visible. Dex accepts the checkpoint when the Worker writes it to its stream,
before the request leaves; a Worker lost in that instant, before Dex stored
the checkpoint, leaves the next attempt without it. That attempt still looks
for the write by its `external_id` before sending, so the checkpoint and the
lookup narrow this window without closing it: a write Gorgias does not show
yet can still be sent twice. A crash after the checkpoint but before the
request left reports `uncertain` for a request Gorgias never received.

Because the connector owns `external_id` on the tickets it creates, store an
application correlation ID in a tag or in the Flow's own Attributes instead.

## Errors and rate limits

A non-2xx response never exposes Gorgias's message text. A Failure repeats
only a snake_case code in `error.msg`, such as `ticket_merged`, and up to five
field names under `error.data`, such as
`Gorgias rejected the request (HTTP 400) [fields: subject]`. A token that
contains the API key is dropped.

| Response | Reads and updateTicket | createTicket and addNote |
| --- | --- | --- |
| 400 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | same |
| 409, 413, other 4xx | `providerRejected` | `providerRejected` |
| 3xx, such as the 301 for a merged customer or ticket | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry after `Retry-After` | Retry after `Retry-After`, looks up by `external_id`, then may resend |
| connection refused before sending | Retry | Retry, looks up by `external_id`, then may resend |
| 408, 5xx, lost or unreadable response | Retry, after `Retry-After` when present | look up by `external_id`: `created`/`added`, or `uncertain` after repeated lookups |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | look up by `external_id` |
| invalid input or connection credentials | `defect`, with no request | `defect`, with no request, unless an earlier attempt sent it: then the lookup is retried and ends in `created`/`added` or `uncertain` |

Gorgias limits API-key integrations to 40 requests per 20-second window, a
10-second window for Enterprise accounts, and answers 429 with `Retry-After`
in seconds. Each Receipt carries the Call ID, the ticket, message, or customer
ID, the `X-Gorgias-Account-Api-Call-Limit` value such as `10/40` as
`apiCallLimit` metadata, and an `X-Request-Id` header when Gorgias sends one.

## Not in this release

### Triggers

Gorgias pushes events only through HTTP integrations: an administrator
configures a URL, method, headers, and a templated body that Gorgias sends on
`ticket-created`, `ticket-updated`, `ticket-message-created`, and similar
events. Gorgias documents no request signature, so a receiver could
authenticate a call only by a static header the integration sends in plain
configuration, and the templated body carries no event ID unless the
administrator adds one. `sdkgo/webhooktrigger` needs a verifiable request and
a stable event ID, so the connector declares no Trigger. A Trigger would need
a signed or otherwise verifiable source, or a durable per-binding poll cursor
over `GET /api/events`, which `sdkgo` does not have yet. Until then, poll with
`searchTickets` and `updatedSince`.

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

## Example

[`examples/triage-issue`](examples/triage-issue) is a runnable Dex Web
**Start Flow** example that uses all six operations: it finds the customer,
follows up on their open ticket about an issue or creates one, sets its
priority, adds one internal triage note, and optionally emails the customer
an acknowledgement.

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
newest-first messages and truncation, escaped HTML bodies, the dispatch
checkpoint and its clearing, the `external_id` lookup after a lost response or
Worker and before a later attempt resends, repeated lookups and `Retry-After`,
credential failures after a dispatch, reply routing from a known helpdesk
address, the ticket read again after a partly applied update, only-changed-field updates and set-based tag changes,
Gorgias error codes and field names without message text, `Retry-After`,
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
`X-Request-Id` header; and the Settings > Account > REST API path in the setup
guidance.
