# Re:amaze Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Re:amaze; no live Re:amaze account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Re:amaze Connector reads and writes Re:amaze conversations from Dex Flows
through the Re:amaze JSON API v1. It is a thin sibling of the
[Help Scout](../helpscout/README.md) and [Intercom](../intercom/README.md)
conversation desks and the [Zendesk](../zendesk/support/README.md) and
[Freshdesk](../freshworks/freshdesk/README.md) ticket desks: the same branch
names, the same requester, tag, and status shapes, and Re:amaze's own
integers. It exposes these operation-specific Dex Step factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `reamaze.NewSearchConversationsStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `reamaze.NewGetConversationStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `reamaze.NewFindContactByEmailStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `reamaze.NewCreateConversationStep` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `reamaze.NewUpdateConversationStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `reamaze.NewReplyToConversationStep` | Mutation | sync | `replied` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout. Reads and `updateConversation` use the one-hour default retry budget; `createConversation` and `replyToConversation` keep five attempts within five minutes. Re:amaze
limits each API token per minute, so a 429 without `Retry-After` waits one
minute before the retry. `createConversation` and
`replyToConversation` use sync durability because Re:amaze has no idempotency
key; see [Duplicate safety](#duplicate-safety).

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Re:amaze setup

A connection needs three values:

- **brand**, non-secret configuration: the `acme` in `https://acme.reamaze.io`.
  Every request goes to `https://{brand}.reamaze.io/api/v1`. Re:amaze scopes
  API requests by the brand host, which **Settings > Brands** lists; a custom
  help-center domain does not work. The connector accepts only lowercase
  letters, digits, and hyphens, 1 to 63 characters, before it builds any URL.
- **email**, a non-secret credential field: the login email of the staff user
  the connector acts as.
- **api_token**, a secret credential field: that user's API token, from
  **Settings > Developer > API Token > Generate New Token**. Every Re:amaze
  user has their own token, so the token and email must belong to the same
  user.

The connector authenticates every request with HTTP Basic,
`{email}:{api_token}`, and sends `Accept: application/json`, as the
[API introduction](https://www.reamaze.com/api) documents. Replies, notes, and
changes are attributed to that user, and the user's role limits what the
connector can read and change. Credentials are reread before every provider
call, so replacing them in Dex Web takes effect without a restart. The brand
and response limit are startup configuration.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/triage-conversation/main.go`](examples/triage-conversation/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := reamaze.NewProjectConnection(project, triageconversation.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `email` and `api_token`.

## Statuses, visibilities, and origins

The connector takes and returns Re:amaze's own integers and never maps them to
another vocabulary. A Flow that needs words maps them itself, in the open; the
example's `DescribeConversationStatus` shows both, such as `Responded (1)`.

| Re:amaze status | Value | Go constant |
| --- | --- | --- |
| Open | 0 | `ConversationStatusOpen` |
| Responded | 1 | `ConversationStatusResponded` |
| Done | 2 | `ConversationStatusDone` |
| Spam | 3 | `ConversationStatusSpam` |
| Archived | 4 | `ConversationStatusArchived` |
| On Hold | 5 | `ConversationStatusOnHold` |
| Auto-Done | 6 | `ConversationStatusAutoDone` |
| AI Agent Assigned | 7 | `ConversationStatusAIAgentAssigned` |
| AI Agent Done | 8 | `ConversationStatusAIAgentDone` |
| Spam (identified by AI) | 9 | `ConversationStatusAISpam` |

Re:amaze's statuses are fixed, so inputs accept only 0 to 9
(`reamaze.ConversationStatuses()`), and Results pass through any value
Re:amaze returns. Open is 0, so status inputs are pointers: nil keeps the
status. A customer reply reopens a Done conversation to Open, a staff reply
sets Responded, and On Hold carries a reminder time, `holdUntil`.

A message's `visibility` is 0 for a regular message the customer sees, 1 for
an internal note, and 2 for Re:amaze's collision-detected message, which is
listed but never written; `isInternalNote` is true for 1. A message's `origin`
and a channel's `type` are Re:amaze's integers, such as origin 1 (email) or 7
(API) and channel type 1 (email) or 6 (chat), passed through as documented in
`GET /messages` and `GET /channels`.

Re:amaze exposes no SLA due time. `lastCustomerMessageAt` is the clock for a
first- or next-response target, and `lastStaffMessageAt`, which Re:amaze
documents only on single conversation reads, shows when staff last answered.

## Operations

### searchConversations

`searchConversations` reads one page of `GET /conversations`, 30
conversations per page (`SearchPageSize`), with Re:amaze's list filters:

- `filter`: blank for every unarchived conversation, Re:amaze's default, or
  `open`, `unassigned`, `archived`, or `all`.
- `requesterEmail`: Re:amaze's `for`, the conversations relevant to that user.
  For a customer this is every conversation the customer can see, including
  conversations the customer was copied on, so confirm the requester before
  writing, as the example does.
- `tags`: sent comma-separated as `tag`, so a tag cannot contain a comma.
- `channel`: a channel slug, Re:amaze's `category`.
- `sort`: blank orders by creation, `updated` by the latest customer update,
  and `changed` by any update or status change.
- `customerMessageSince` and `customerMessageUntil`: RFC 3339 instants for
  Re:amaze's `start_date` and `end_date`, which filter by the time of the
  latest customer message.
- `page`: 1 to 10,000 (`MaxSearchPage`, a connector bound); `nextPage` is zero
  after Re:amaze's `page_count`.

Re:amaze has no status filter beyond `filter`, so a Flow keeps the statuses it
wants from the page. Search pages omit `firstMessage` to stay small.

### getConversation

`getConversation` reads `GET /conversations/{slug}`, then the first page of
`GET /conversations/{slug}/messages`, which Re:amaze sorts newest first.
`messageLimit` is 1 to 30, 10 by default, and `hasMoreMessages` reports older
messages. The conversation's slug is its ID: Re:amaze documents it as the
unique identifier, and the connector accepts letters, digits, hyphens, and
underscores only, so an ID can never change the request path. Every first
message and message body is cut at 16 KiB (`reamaze.MaxTextBytes`) on a UTF-8
boundary and flagged `isFirstMessageTruncated` or `isBodyTruncated`.

### findContactByEmail

`findContactByEmail` reads the first page of
`GET /contacts?q={email}&type=email` and keeps the contacts whose email equals
the address without regard to letter case, because Re:amaze's `q` searches
names and emails. Contacts belong to the account, not one brand. `notFound`
with `hasMoreCandidates` means the first 30 candidates held no exact match but
Re:amaze listed more pages.

### createConversation

`createConversation` sends `POST /conversations` on behalf of a customer with
the subject, the first message, a channel slug (`category`), the requester's
email and optional name, and optional status, `holdUntil`, tags, assignee
email, and `shouldSuppressNotifications`. Re:amaze adds a contact for an
unknown email. The message is sent unchanged; Re:amaze formats message bodies
as Markdown. The conversation also carries the data attribute
`dex_dispatch_key` (`reamaze.DispatchKeyDataAttribute`), set to `dex-` and the
Step's Call ID, which agents see among the conversation's data attributes.

### updateConversation

`updateConversation` sets the status, `holdUntil`, and assignee, and adds or
removes tags. It reads `GET /conversations/{slug}`, then sends
`PUT /conversations/{slug}` with only the fields that differ, and the complete
resulting `tag_list` when tags change. Tags and assignee emails match without
regard to letter case. When nothing differs, it selects `updated` with
`wasAlreadyApplied: true` and writes nothing. `holdUntil` requires status 5
and is always written, because Re:amaze does not return it to compare. This
release cannot unassign a conversation.

### replyToConversation

`replyToConversation` sends `POST /conversations/{slug}/messages` with
`visibility` 1 for an internal note (`isInternalNote`) or 0 for a public reply,
which Re:amaze sends to the customer through the conversation's channel.
`shouldSuppressNotifications` and `shouldSuppressAutoResolve` map to Re:amaze's
`suppress_notifications` and `suppress_autoresolve`; the second stops Re:amaze
from marking the conversation Done because a staff user wrote. Every message
carries `origin_id` set to `dex-` and the Step's Call ID, which Re:amaze
documents as a unique message identifier that helps prevent duplicates.

## Mocks

`reamazemock` supplies one default happy outcome and the definite business
branches for each of the six operations. Its defaults describe Jane's new
billing issue: a known contact, three conversation pages containing resolved,
wrong-tag, and wrong-requester decoys, one created support conversation, and
one internal triage note. The runnable Flow's real Dex mock
test records calls and checks application behavior:

```go
mock := reamazemock.New(t, ConnectionName)
harness := newTriageHarnessForConnection(t, mock.Connection())
outcome := harness.runTriage(t, "mock-new-issue", integrationIssueInput())
require.Equal(t, TriageConversationOpened, outcome.Action)
```

## Duplicate safety

`updateConversation` sets absolute values and safely retries. `createConversation`
and `replyToConversation` use the shared `sdkgo/singledispatch` fence with sync
Execute durability. The fence checks the prior attempt's heartbeat before
credentials are loaded, claims the write, bounds its HTTP request inside the
Step deadline, and permits one send per attempt. A missing, stale, foreign,
or claimed checkpoint never authorizes a resend. A 429, 503, or provably
undispatched connection permits one retry; a 409 triggers read-back. A 2xx
with an unusable body triggers read-back in the same attempt without another
POST. A transient read-back failure retries as a read, while an absent record
selects `uncertain`.

The create request stores the Step's key in conversation `data`, then a later
attempt searches for that key and confirms it on the conversation detail when
the list omits `data`. The reply stores the key as `origin_id` and reads the
newest message page. If these reads cannot confirm an earlier write, the
operation selects `uncertain`; a transient read failure retries. Re:amaze
[documents `origin_id` as a duplicate prevention aid](https://www.reamaze.com/api/post_messages),
but does not specify an enforced deduplication window or conflict behavior.
That guarantee remains unverified, so a possibly accepted reply is never
resent. Re:amaze [documents conversation data filters](https://www.reamaze.com/api/get_conversations),
but a filtered or newest page may omit a created record; absence cannot prove
that another POST is safe.

A heartbeat frame can be lost between the Worker and Dex storage. The shared
fence fails closed when the claim is missing on a later attempt, so no second
POST follows that ambiguity. Keep these two Steps sync; async local-phase
heartbeats are not visible to later attempts.

Before upgrading an in-flight v0.21.2 create or reply, reconcile it in
Re:amaze by its conversation data key or message `origin_id`. The shared fence
cannot read legacy dispatch markers; a v0.21.2 marker or a heartbeat cleared
after 429 fails closed to `uncertain` on the next attempt, even when the
provider did not apply the request.

## Errors

A non-2xx response never exposes Re:amaze's body: Re:amaze documents 422 with
"a JSON body explaining the error" but not its shape, so a Failure names only
the HTTP status, such as `Re:amaze rejected the request (HTTP 422)`.

| Response | Reads and updateConversation | createConversation and replyToConversation |
| --- | --- | --- |
| 400, 422 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | same |
| 404 during reply read-back | N/A | `uncertain`: the conversation may have been removed after accepting the reply |
| 409 | `providerRejected`, `CONFLICT` | Retry as a read-back, never a resend |
| other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry after `Retry-After`, or after one minute without it | Retry after `Retry-After` or one minute, then send |
| connection refused before sending | Retry | Retry, then send |
| 503 | Retry, after `Retry-After` when present | Retry with the next attempt’s fence marker |
| 408, other 5xx, lost or unreadable response | Retry, after `Retry-After` when present | Retry as a read-back, never a resend |
| oversized, interrupted, or credential-reflecting 2xx | `invalidResponse` or Retry for a read | Read back in the same attempt; `uncertain` if no record confirms acceptance |
| malformed 2xx | `invalidResponse` | Read back in the same attempt; `uncertain` if unconfirmed |
| connection credentials that cannot be read yet, such as a storage outage | Retry, with no request | Retry, with no request |
| invalid input or unusable connection credentials | `defect`, with no request | `defect`, `AUTHENTICATION` with a reconnect hint before send; `uncertain` with a reconnect hint after a possible send |

The Receipt carries the Call ID and idempotency key. It includes a conversation
slug when the connector knows one, including a saved slug on a read-back
credential failure, and an `X-Request-Id` header when Re:amaze sends one.

## Not in this release

### Triggers

Re:amaze documents no event webhook subscription in its API reference. The
only signed outbound requests it documents belong to custom SMS and voice
channels: a channel-transport protocol that posts a staff reply to the
channel's own gateway, signed with `X-Reamaze-Hmac-SHA256` over the body with
the channel's shared secret, with no event ID or event type. That is not a
conversation event feed, so the connector declares no Trigger. A push Trigger
would need a documented event subscription with a stable event ID; a polling
Trigger would need a durable per-binding cursor, which `sdkgo` does not have.
Until then, poll with `searchConversations`, `sort: changed`, and
`customerMessageSince`.

### Studio pickers

A channel or staff picker would list `GET /channels` or `GET /staff`, but
Studio setup commands declare a fixed HTTPS host and support only bearer or
raw-header credentials. Re:amaze's host is per brand and its API needs HTTP
Basic, so channel slugs and staff emails are operation input. Settings >
Channels and Settings > Staff list them.

### Conflict detection

Re:amaze documents no conditional update, so a concurrent change by another
agent between `updateConversation`'s read and write is overwritten for the
fields this Step sets.

## Example

[`examples/triage-conversation`](examples/triage-conversation) is a runnable
Dex Web **Start Flow** example that uses all six operations: it resolves the
customer by email, finds the customer's unresolved conversation about an issue
or creates one, reopens and tags it, and adds one internal triage note.

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
go run ./cmd/connectorctl validate connectors/reamaze/connector.yaml
go run ./cmd/connectorctl generate --check connectors/reamaze/connector.yaml
```

The provider fakes cover HTTP Basic and `Accept: application/json`, brand
validation, list filters and pagination, the newest-first message page, exact
contact matching, the dispatch checkpoint and its clearing, read-back by data
attribute and by `origin_id`, only-changed-field updates and complete tag
lists, status-only errors without body text, `Retry-After` and its one-minute
fallback, redirects, oversized, malformed, and credential-reflecting
responses, and invalid or unreadable credentials.

No live Re:amaze account was used. The following live behavior is
unverified: whether `GET /conversations` honors `data[key]=value` for a
conversation created seconds earlier, whether its list entries include the
`data` hash, which Re:amaze shows only on single conversation reads, and
whether `POST /conversations` stores a `data` hash; whether
`POST /conversations/{slug}/messages` stores and
returns a client `origin_id`, and whether Re:amaze rejects or merges a
repeated one; the 422 body shape; whether 429 is always returned before a
write is applied and whether it carries `Retry-After`; the status of an
unknown brand host or slug; whether `for` with a customer email includes
conversations the customer was copied on; how `tag` combines several tags;
tag letter-case handling and whether `tag_list` replaces the tag set; whether
an internal note triggers auto-resolve; whether every slug uses only letters,
digits, hyphens, and underscores, which the connector requires; the
`X-Request-Id` header; and the Settings paths in the setup guidance.
