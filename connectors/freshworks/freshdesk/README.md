# Freshdesk Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk/go@v0.23.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/freshworks/freshdesk`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Freshdesk; no live Freshdesk account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Freshdesk Connector reads and writes Freshdesk tickets from Dex Flows
through Freshdesk API v2. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `freshdesk.NewSearchTicketsStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `freshdesk.NewGetTicketStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `freshdesk.NewCreateTicketStep` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `freshdesk.NewUpdateTicketStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `freshdesk.NewAddNoteStep` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout. Reads and `updateTicket` use the one-hour default retry budget; `createTicket` and `addNote` keep five attempts within five minutes.
`createTicket` and `addNote` use sync durability because Freshdesk has no
idempotency key; see [Duplicate safety](#duplicate-safety).

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Freshdesk setup

A connection needs two values:

- **domain**, non-secret configuration: the `acme` in
  `https://acme.freshdesk.com`. Every request goes to
  `https://{domain}.freshdesk.com/api/v2`. Freshdesk serves its API only on
  freshdesk.com domains, so a custom support domain does not work.
- **api_key**, a secret credential field: the API key of the agent the
  connector acts as. Tickets, notes, and replies it writes are authored by
  that agent, and the agent's role limits what it can read and change.

The agent signs in to the helpdesk, selects the profile picture at the top
right, chooses **Profile Settings**, then **View API key**, and completes the
captcha, as Freshdesk's
[API key article](https://support.freshdesk.com/support/solutions/articles/215517)
describes. Freshdesk shows the key only to a verified agent, and the Sprout
and Free plans have no API access. The connector authenticates with HTTP Basic,
the key as the user name and `X` as the password, as Freshdesk's API
documentation specifies. Resetting the key in Freshdesk revokes it and
disconnects every app that uses it.

Credentials are reread before every provider call, so replacing a key in Dex
Web takes effect without a restart. The domain and response limit are startup
configuration.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/triage-issue/main.go`](go/examples/triage-issue/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := freshdesk.NewProjectConnection(project, triageissue.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `api_key`; the connector resolves it for
every call and rejects anything else without repeating the value.

## Statuses, priorities, and sources

The connector takes and returns Freshdesk's own integers and never maps them to
another vocabulary. A Flow that needs words maps them itself, in the open; the
example's `DescribeTicketPriority` shows both, such as `High (3)`.

| Freshdesk status | Value | Go constant |
| --- | --- | --- |
| Open | 2 | `TicketStatusOpen` |
| Pending | 3 | `TicketStatusPending` |
| Resolved | 4 | `TicketStatusResolved` |
| Closed | 5 | `TicketStatusClosed` |
| Waiting on Customer (custom) | 6 | `TicketStatusWaitingOnCustomer` |
| Waiting on Third Party (custom) | 7 | `TicketStatusWaitingOnThirdParty` |

Freshdesk fixes 2 through 5, which cannot be deleted
(`freshdesk.FixedTicketStatuses()`). Statuses 6 and 7 are custom statuses that
Freshdesk's API documentation shows in its ticket-field sample and in its "all
unresolved tickets" filter, `status:2 OR status:3 OR status:6 OR status:7`; an
account may lack them, rename them, or add more under **Admin > Workflows >
Ticket Fields**. The account's own list is the `choices` of the
`default_status` field from `GET /api/v2/ticket_fields`. Inputs therefore
accept any status from 2 up and let Freshdesk reject an unknown one as
`providerRejected`, and Results pass through every value Freshdesk returns.

| Freshdesk priority | Value | Go constant |
| --- | --- | --- |
| Low | 1 | `TicketPriorityLow` |
| Medium | 2 | `TicketPriorityMedium` |
| High | 3 | `TicketPriorityHigh` |
| Urgent | 4 | `TicketPriorityUrgent` |

Priorities are built in and cannot be edited, so inputs accept only 1 to 4
(`freshdesk.TicketPriorities()`). A ticket's `source` uses Freshdesk's values:
1 Email, 2 Portal, 3 Phone, 7 Chat, 9 Feedback Widget, 10 Outbound Email. A
conversation's `source` is 0 for a reply and 2 for a note.

## Operations

### searchTickets

`searchTickets` builds one Freshdesk filter query from typed filters and reads
one page of `GET /api/v2/search/tickets?query="..."`:

- `statuses`: any of the given statuses (`(status:2 OR status:3)`).
- `priorities`: any of the given priorities (`priority:4`).
- `tags`: any of the given tags (`tag:'billing'`).
- `updatedSinceDate`: a UTC calendar date written `YYYY-MM-DD`
  (`updated_at:>'2026-01-21'`, which Freshdesk documents as greater than or
  equal). Freshdesk's filter query compares dates, not instants.
- `requesterEmail`: the requester's email address. Freshdesk's filter query
  has no requester field, so the connector first reads
  `GET /api/v2/contacts?email=...`, then keeps only that contact's tickets
  from the page. A customer without a contact returns an empty page without
  searching.

At least one of `statuses`, `priorities`, `tags`, or `updatedSinceDate` is
required. `BuildTicketFilterQuery` returns the query for review. Freshdesk's
documented limits shape every page:

- a page holds at most 30 tickets (`SearchPageSize`) and `page` is 1 to 10
  (`MaxSearchPage`), so a search reaches at most 300 tickets; `nextPage` is
  zero after page 10 even when `totalMatched` is larger;
- the query, with its enclosing double quotes, is at most 512 characters
  (`MaxFilterQueryLength`);
- `totalMatched` is Freshdesk's `total`, counted before the `requesterEmail`
  filter, so a page can hold fewer than 30 tickets, even none, while
  `nextPage` is set. Combine `requesterEmail` with narrowing filters such as an
  issue tag;
- archived tickets are excluded, and Freshdesk indexes changes within a few
  minutes, so search is not a duplicate check immediately after a write;
- search pages omit descriptions, and Freshdesk's documented search results
  carry no tags, so read the ticket before relying on its tags.

Tags must be 1 to 64 letters, digits, spaces, and `_ . / + -`, so a tag can
never change the query.

### getTicket

`getTicket` reads `GET /api/v2/tickets/{id}?include=requester`, then the first
conversations with `GET /api/v2/tickets/{id}/conversations?per_page={limit}&page=1`.
`conversationLimit` is 1 to 20, 10 by default. Conversations are oldest first,
the order Freshdesk documents for embedded conversations, replies and notes
alike with `isPrivate` set, and `hasMoreConversations` reports Freshdesk's
`Link: rel="next"` header. A missing ticket selects `notFound`.

Every description and conversation body is Freshdesk's plain-text field
(`description_text`, `body_text`), cut at 16 KiB (`freshdesk.MaxTextBytes`) on
a UTF-8 boundary and flagged `isDescriptionTruncated` or `isBodyTruncated`, so
Step state stays bounded.

### createTicket

`createTicket` sends `POST /api/v2/tickets` with the subject, a plain-text
description, the requester's email and optional name, and optional status,
priority, type, tags, group, and agent (`responderId`). Freshdesk adds a contact
for an unknown email. Zero values leave Freshdesk's defaults: status Open (2),
priority Low (1), and source Portal (2). The description is HTML-escaped with
line breaks kept, so text is never interpreted as markup.

### updateTicket

`updateTicket` sets status, priority, group, and agent, and adds or removes
tags. It reads `GET /api/v2/tickets/{id}`, then sends
`PUT /api/v2/tickets/{id}` with only the fields that differ, and the complete
resulting tag list when tags change. Tags match without regard to letter case.
When nothing differs, it selects `updated` with `wasAlreadyApplied: true` and
writes nothing. A spam or deleted ticket returns 405 and a missing required
field 400, both `providerRejected`. This release cannot clear an agent or
group.

### addNote

`addNote` adds one private note with
`POST /api/v2/tickets/{id}/notes` and `private: true`, or, with
`isPublicReply`, one reply with `POST /api/v2/tickets/{id}/reply`, which
Freshdesk emails to the requester. The body is plain text, escaped like the
ticket description. A missing ticket selects `notFound`.

## Duplicate safety

Freshdesk API v2 documents no idempotency key, so a repeated create or note is
a duplicate ticket, a second internal note, or a second email to the customer.
Dex retries a Step after a lost Worker, so the operations take these positions:

- **updateTicket** is safe to repeat. Its write sets absolute values computed
  from a fresh read, so a second dispatch sends the same values or finds them
  applied without another write. It keeps async durability, and every
  unconfirmed outcome, including a 5xx or a lost response, is retried.
- **createTicket** and **addNote** run with sync durability, so Dex never sends
  a second attempt while the first is in flight. The operation first checks the
  previous heartbeat, resolves credentials, then records the current Call ID
  and attempt before sending. A 429, 503, or traced DNS, connect, or TLS failure
  before connection records which next attempt may retry. A missing or stale
  checkpoint on a later attempt selects `uncertain` without sending; Dex does
  not acknowledge heartbeat persistence.
- A write starts only when its request window retains at least one tenth of
  the attempt budget, capped by the 30-second Execute timeout. A full attempt
  therefore needs at least three seconds for the request, plus one second
  reserved before the observed Step deadline to record the outcome. A skipped
  request records a permit for only the next attempt and returns Retry.
- A transient credential-store read failure returns Retry without sending and
  records a next-attempt permit. A missing or invalid credential selects
  `defect` on the first attempt; after a permitted retry it returns Retry, so
  credential trouble cannot hide a possibly accepted earlier write.
- A 408, a 5xx other than 503, a lost or unreadable response, and an invalid or oversized
  2xx select `uncertain`: the ticket or note may exist, and the connector never
  resends it. The example leaves `uncertain` unwired, so the Flow fails;
  an operator can inspect Freshdesk before deciding what to do next.

[Freshdesk API v2](https://developers.freshdesk.com/api/#create_ticket) documents ticket
creation; [filter tickets](https://developers.freshdesk.com/api/#filter_tickets)
and [ticket notes](https://developers.freshdesk.com/api/#list_all_ticket_notes)
document search and conversation reads. The API does not
document a per-request idempotency key, a ticket lookup by connector Call ID,
or a conversation marker field. Ticket search indexing can lag by minutes.
An account-specific ticket custom field would change the customer's ticket
form and still leave note or reply correlation unresolved. A search miss
cannot prove that a possibly accepted write did not happen.

Keep these two Steps sync so results are committed promptly. A checkpoint lost before
Dex stored it, or a crash before the request left, can make an attempt report
`uncertain` for a request Freshdesk never received; that direction is safe.

## Errors

A non-2xx response never exposes Freshdesk's `description` or `message` text.
A Failure repeats only Freshdesk's machine-readable `code` and up to five
`errors` entries as `field=code`, such as
`Freshdesk rejected the request (HTTP 400) [errors: status=invalid_value]`. A
token that contains the API key is dropped.

| Response | Reads and updateTicket | createTicket and addNote |
| --- | --- | --- |
| 400 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | same |
| 405, 409, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry after `Retry-After` | Retry after `Retry-After` |
| connection refused before sending | Retry | Retry |
| 503 | Retry, after `Retry-After` when present | Retry after a next-attempt checkpoint |
| 408, other 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input or terminal connection credentials | `defect`, with no request | `defect` on the first attempt; Retry after a permitted retry |
| transient credential-store failure | Retry, with no request | Retry with a next-attempt permit, with no request |

The Receipt carries the Call ID, the ticket or conversation ID, and
Freshdesk's `X-Request-Id` header when present.

## Not in this release

### Triggers

Freshdesk sends webhooks only from automation rules: an administrator adds a
**Trigger webhook** action and chooses the URL, method, encoding, content, and
optional custom headers. Freshdesk documents no request signature, so a
receiver can authenticate a webhook only by a static shared header value that
the rule sends in plain configuration, and failed calls are retried every 30
minutes up to 48 times. The connector declares no Trigger until a signed or
otherwise verifiable source exists. Until then, poll with `searchTickets` and
`updatedSinceDate`.

### Studio pickers

A group, agent, or status picker would list `GET /api/v2/groups`,
`GET /api/v2/agents`, or `GET /api/v2/ticket_fields`, but Studio setup commands
declare a fixed HTTPS host and support only bearer or raw-header credentials.
Freshdesk's host is per helpdesk and an API key needs HTTP Basic, so the
connection uses guided inputs. Group and agent IDs are operation input; the
`GET /api/v2/groups` and `GET /api/v2/agents` responses list them.

### Conflict detection

Freshdesk documents no conditional update, so `updateTicket` has no
`expectedUpdatedAt` check: a concurrent change by another agent between the
read and the write is overwritten for the fields this Step sets.

## Example

[`examples/triage-issue`](go/examples/triage-issue) is a runnable Dex Web
**Start Flow** example that uses all five operations: it finds the customer's
unresolved ticket about an issue or creates one, sets its priority, and adds
one private triage note.

## Mocks

The generated `freshdeskmock` package gives each operation one realistic
default and named definite rejection cases. Its defaults describe Jane's
billing issue: search page 1 is empty, page 2 finds ticket 1201, getTicket
confirms it, updateTicket reprioritizes it, and addNote adds the triage note.
createTicket has a default for the alternate new-issue path. Tests should
assert call inputs and counts; unscripted calls otherwise receive defaults.
The runnable example's real-Dex mock test uses:

```go
mock := freshdeskmock.New(t, ConnectionName)
harness := newTriageHarnessForConnection(t, mock.Connection())
input := integrationIssueInput()
outcome := harness.runTriage(t, "mock-default", input)
```

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
go run ./cmd/connectorctl validate connectors/freshworks/freshdesk/connector.yaml
go run ./cmd/connectorctl generate --check connectors/freshworks/freshdesk/connector.yaml
```

The provider fakes cover Basic authentication, filter-query building and quoting,
page limits, the contact lookup and requester filter, the requester embed and
oldest-first conversations, escaped HTML bodies, attempt-scoped dispatch and
retry checkpoints, only-changed-field updates and complete tag lists, Freshdesk error
codes and field tokens without message text, `Retry-After`, redirects,
oversized, malformed, and credential-reflecting responses, and invalid
credentials.

No live Freshdesk account was used. The following live behavior is
unverified: whether `GET /api/v2/contacts?email=` matches only a contact's
primary email and how it treats agents; whether filter-query results include
tags; how the filter query treats a status value the account does not define;
whether `PUT /api/v2/tickets/{id}` replaces the tag list with the sent list;
whether `/conversations` is ordered oldest first and sets the `Link` header as
other list endpoints do; whether 429 responses are always returned before a
write is applied; whether a 503 always means no write was accepted; the
`X-Request-Id` header; deadline and checkpoint behavior against a real
Freshdesk account; and the Profile Settings path in
the setup guidance.
