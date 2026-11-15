# Hiver Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/hiver/go@v0.24.0`.

The Python package is `dex-connector-hiver==0.1.0`. It implements all six operations with generated Step factories and does not implement the `conversationReceived` Trigger, which is Go-only. Its [runnable conversation claim Flow](python/examples/claim_conversation/flow.py), [Worker entrypoint](python/examples/claim_conversation/main.py), and [package guide](python/README.md) show the static `hiver-support` connection and local Dex verification. Python replay uses the Go-recorded [conformance scenarios](../../conformance/connectors/hiver/scenarios.json), including multipart note and shared draft requests. Live Hiver authorization and API behavior remain unverified.

For `addNote`, Python preserves Hiver's `created_at` through microseconds. A returned timestamp with nonzero sub-microsecond digits selects `uncertain` with a `created_at` protocol failure because the accepted note cannot be safely resent. The [Python package guide](python/README.md) describes this limit.

Moved from `github.com/superdurable/dex-connectors-library/connectors/hiver`.

> **Verification status: Dex-integrated in CI, not live.** The example runs
> on a real Dex stack against a Hiver stand-in. See
> [Verification](#verification) for what is and is not verified.

The Hiver Connector reads and changes conversations in Hiver shared inboxes
from Dex Flows through the Hiver API v1 at `https://api2.hiverhq.com/v1`,
documented at <https://developer.hiverhq.com/hiver-api>. It exposes these
operation-specific Dex Step factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `hiver.NewListInboxesStep` | Query | async | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `hiver.NewListConversationsStep` | Query | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `hiver.NewGetConversationStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `hiver.NewUpdateConversationStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `hiver.NewAddNoteStep` | Mutation | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `hiver.NewCreateSharedDraftStep` | Mutation | sync | `created` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Reads, `addNote`,
and `createSharedDraft` use a 30-second Execute timeout; `updateConversation`,
which may make up to eight spaced requests, uses 60 seconds. Reads and `updateConversation` use the one-hour default retry budget; `addNote` and `createSharedDraft` keep five attempts within five minutes and use sync durability
because Hiver has no idempotency key; see [Duplicate safety](#duplicate-safety).

The `conversationReceived` Trigger starts a Flow for each conversation that
newly appears in one shared inbox, which the binding picks with the
`inboxPicker` unit; see [conversationReceived Trigger](#conversationreceived-trigger).

## Hiver setup

A connection needs one secret and has four optional settings:

- **api_key**, a secret credential field. Only a Hiver admin can create it, at
  **Admin Panel > Integrations > Developer APIs > Create an API key** in Hiver
  (the web app at <https://v2.hiverhq.com/>, or Hiver in Gmail), as
  <https://help.hiverhq.com/hiver-api/hiver-api> describes. After choosing
  **Create and generate API key**, turn on the app's toggle and copy the key.
  The key has admin access to every shared inbox of the account, and the
  connector sends it only as `Authorization: Bearer` to
  `https://api2.hiverhq.com/v1`. Hiver's Pro and Elite plans include API
  access, as does the trial. Delete the key in the same page to revoke it.
- **maxResponseBytes**, optional: the largest response one request reads,
  4 MiB by default.
- **requestIntervalMilliseconds**, optional: the shortest time between two
  requests of this connection in one Worker, 1000 by default; see
  [Request spacing](#request-spacing).
- **pollInterval**, optional: the delay between `conversationReceived` polls of
  each binding, a Go duration of at least 1s, 5m by default.
- **pollPagesPerPoll**, optional: the pages of 100 conversations one poll
  reads, from 2 to 5, 3 by default.

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/claim-conversation/main.go`](go/examples/claim-conversation/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := hiver.NewProjectConnection(project, claimconversation.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `api_key`; the connector resolves it for
every call and rejects a value that is not printable ASCII without spaces,
without repeating it.

## Ticket vocabulary

Hiver is a shared inbox over Gmail: a **conversation** is one customer email
thread in a **shared inbox**. The connector keeps Hiver's own vocabulary and
maps it to the other desk connectors as follows:

| Desk concept | Hiver | Connector |
| --- | --- | --- |
| Ticket ID | Hiver conversation ID; Hiver also accepts the shared mailbox user's Gmail thread ID | `conversationId` input, `Conversation.ID` and `GmailThreadID` output |
| Queue | Shared inbox, always required | `inboxId`; `listInboxes` lists them with their email addresses |
| Status | `open`, `pending`, closed | `ConversationStatus` `open`, `pending`, `closed` |
| Assignee | Inbox user, set by email | `assigneeEmail` in, `Assignee{Type, ID}` out |
| Tags | Inbox tags, set by tag ID | `applyTagNames`/`removeTagNames` in, `TagIDs` out |
| Internal note | Note | `addNote` |
| Public reply | Shared draft that a Hiver user sends | `createSharedDraft` |
| Requester, priority, SLA, subject | Not in Hiver's API | Not available |

Hiver's update documents the status values `open`, `pending`, and `close`. The
connector sends `close` for `ConversationStatusClosed` and reports both `close`
and `closed` as `closed`; it passes any other status Hiver returns through
unchanged. Hiver's API exposes no requester, priority, subject, timestamps, or
SLA fields, and its conversation list has no filter, so a Flow filters each
page itself.

## Operations

### listInboxes

`GET /v1/inboxes` returns one page of shared inboxes with `id`, display name,
email address, channel type, and whether Hiver can still read the mailbox.
`PageSize` is 10 to 100 (zero sends 50), and `NextPageToken` is Hiver's opaque
`next_page` token, empty after the last page. Hiver's source user is not
carried. A Flow resolves the inbox a person names by email address, as the
example does, instead of asking for an opaque ID.

### listConversations

`GET /v1/inboxes/{inbox_id}/conversations` returns one page of conversations
with status, assignee, tag IDs, Gmail thread ID, and private permalink. It
takes the same page inputs as `listInboxes`. An unknown inbox selects
`notFound`.

### getConversation

`GET /v1/inboxes/{inbox_id}/conversations/{conversation_id}` returns the
conversation and its message IDs in Hiver's order, which Hiver does not
document. It accepts a Hiver conversation ID or a Gmail thread ID. Hiver
documents this response as a one-element `data` array; the connector also
accepts a `data` object. Hiver's public permalink, which anyone with the link
can open, is never carried.

### updateConversation

Sets any of `status`, `assigneeEmail`, `applyTagNames`, and `removeTagNames`:

1. With `assigneeEmail`, `GET .../users/search?email=` must find exactly one
   inbox user with that address, ignoring letter case, or the Step selects
   `providerRejected` before any change.
2. With tag names, `GET .../tags` reads up to five pages of 100 tags, stopping
   once every name has an exact match. Each name must match one tag exactly,
   or else one tag ignoring letter case; an unknown or ambiguous name selects
   `providerRejected` before any change, naming only its position. When the
   five pages did not reach the last tag, the Failure says the name is not
   among the tags on those pages rather than not a tag of the inbox. The
   connector never creates a tag.
3. `PATCH .../conversations/{conversation_id}` sends
   `{"status":{"name":...},"assignee":{"email":...},"tags":{"to_apply":[ids],"to_remove":[ids]}}`
   with only the requested parts.
4. `GET` reads the conversation back. When it does not yet show the status,
   the resolved assignee, or the tag changes, the attempt is retried, which
   sends the same change again. A read-back that Hiver rejects, such as a 401
   after the API key was deleted, selects `invalidResponse`, because Hiver
   already accepted the change.

`providerRejected` means this attempt changed nothing. An earlier attempt that
ended unconfirmed, such as one whose read-back hit a 5xx, may still have
applied the change before Dex retried the Step.

Hiver's API documents no way to unassign a conversation or to clear tags
wholesale.

### addNote

`POST .../conversations/{conversation_id}/notes` sends `content` as
`multipart/form-data`, as Hiver documents. Notes are visible only to Hiver
users. The output is the note's ID, conversation ID, author, and creation time.

### createSharedDraft

`POST /v1/inboxes/{inbox_id}/conversations/shared-drafts` sends `body` and
exactly one of `hiver_message_id` and `gmail_message_id` as
`multipart/form-data`. The Gmail ID may be the shared mailbox user's Gmail
message ID or the message's SMTP `Message-ID`, such as
`<abc123@mail.gmail.com>`, which Hiver accepts as a fallback. Hiver's API
cannot send a reply: a Hiver user reviews, edits, and sends the shared draft,
so this is the connector's public-reply path.

## conversationReceived Trigger

Hiver's API documents no webhooks, and its conversation list documents no
timestamp, filter, or order (<https://developer.hiverhq.com/hiver-api>), so the
Trigger polls `GET /v1/inboxes/{inbox_id}/conversations` with `limit=100` and
`next_page`, using the sdkgo [poll Trigger kit](../../sdkgo/README.md#polling-triggers).
It needs no permission beyond the connection's API key.

- **Binding.** `ConversationReceivedTriggerConfiguration.InboxID` names one
  shared inbox. Dex Web saves it from the `inboxPicker` unit, which lists the
  account's inboxes through the read-only `listInboxes` setup command
  (`GET https://api2.hiverhq.com/v1/inboxes`, bearer API key), or accepts the
  inbox ID typed in.
- **Event.** `ConversationReceivedEvent` carries the inbox ID and the
  `Conversation` as the list reported it: status, assignee, tag IDs, Gmail
  thread ID, and private permalink. The event ID is
  `conversation-<inboxId>-<conversationId>`, so a redelivery starts no second
  Flow. `OccurredAt` is when the poll found the conversation; Hiver reports no
  arrival time.
- **Baseline.** A new binding first walks the whole list, 10 pages per fetch
  without waiting between fetches, and records every conversation ID without
  delivering it. A conversation that arrives during the walk is part of the
  baseline when a page the walk reads later lists it, and is delivered after
  the baseline otherwise. A conversation removed from the list during the walk
  can likewise shift an existing one past a page boundary, and that one is then
  delivered once after the baseline.
- **Polls.** After the baseline, each poll reads the first page plus up to
  `pollPagesPerPoll - 1` pages of a sweep that continues across polls through
  the whole list. A conversation is delivered the first time any of those pages
  lists an ID the binding does not track: on the next poll when Hiver lists new
  conversations first, otherwise within one sweep, about
  `ceil(conversations / (100 * (pollPagesPerPoll - 1)))` polls. IDs that two
  complete sweeps in a row never listed, such as deleted conversations, stop
  being tracked. Hiver does not document how `next_page` tokens behave when the
  list changes, so a conversation removed during a sweep can shift another one
  past a page boundary; waiting for a second sweep keeps that conversation
  tracked instead of delivering it again as new.
- **Cursor.** The durable cursor holds the sweep's `next_page` token and a
  keyed snapshot of tracked IDs. A rejected sweep token (400, 404, 410, or 422)
  restarts the sweep from the first page and keeps every tracked ID, so nothing
  is missed. A cursor for another inbox, or one that cannot be decoded, is
  invalid: the kit records a new baseline and logs `poll cursor invalid;
  reseeding`; conversations between the two are not delivered.
- **Limits.** One binding tracks at most `MaxTrackedConversations` (50,000)
  IDs. An inbox whose list holds more pauses with the ERROR record
  `poll paused: cursor state exceeds bound`, retried every 30 minutes. Each
  page holds at most 100 conversations, so a poll delivers at most 500.
- **Rate limits.** Requests share the connection's request spacing with its
  Steps. A 429 holds the client and pauses every binding of the runner for the
  longer of `Retry-After` and `pollInterval`, or backs off exponentially
  without `Retry-After`. Other failures back off per binding up to 30 minutes,
  or `pollInterval` when longer. A poll costs `pollPagesPerPoll` of the
  account's 5000 daily requests: the defaults use at most 864 per binding per
  day, and a baseline costs one request per 100 conversations.

`hiver.NewProjectConversationReceivedTriggerRunner` loads each binding from the
project configuration and records every conversation in the binding's durable
Trigger inbox before the cursor commits, so a restarted Worker neither misses
nor repeats one. `hiver.NewConversationReceivedTriggerRunner` takes cursor
stores and targets directly, and the generated
`hiver.NewConversationReceivedTrigger` keeps its cursor in memory, so it
records a new baseline after each restart. `hiver.WithLogger` sends the
runner's records, which never carry conversation contents, page tokens, or the
API key, to a logger.

[`examples/new-conversation-intake/main.go`](go/examples/new-conversation-intake/main.go)
wires it:

```go
triggerRunner, err := hiver.NewProjectConversationReceivedTriggerRunner(project, newconversationintake.ConnectionName,
	[]hiver.ProjectConversationReceivedTriggerRoute{{
		BindingName: newconversationintake.ConversationReceivedTriggerBinding, Target: newConversationTarget(client, flow, logger),
	}}, options...)
```

A status, assignee, or tag change Trigger would have to compare every
conversation on every sweep against Hiver's 5000 daily requests, so it is not
offered.

## Request spacing

Hiver limits every account to one request per second and 5000 requests per
day, answers `429 Too Many Requests` above the limit, and warns that continuous
429 retries can block the client IP or API key. Each client therefore spaces
its own request starts at least `requestIntervalMilliseconds` apart, across all
of its concurrent Steps, before sending. A Step that ends while waiting for its
slot sends nothing and is retried. The spacing is per connection and per
Worker process: several Workers or connections on one account share Hiver's
limit, so a 429 is still possible. The rate-limited Step is retried after
`Retry-After` when Hiver sends it, otherwise with its exponential backoff.

A 429 also holds every later request of the client, including requests
already waiting for a slot, so concurrent Steps do not keep retrying at the
full rate. The hold lasts the longer of `Retry-After` and a penalty of four
request intervals that doubles with each further 429, at most one minute; any
other response resets the penalty. A daily limit exhausted by other traffic
keeps answering 429, so the client then sends about one request a minute until
the Steps' retry windows end and they fail. Lower the interval only after
Hiver raises your account's limit.

## Duplicate safety

Hiver API v1 documents no idempotency key, and it has no way to list a
conversation's notes or drafts, so a repeated note or draft is a duplicate that
the connector cannot detect. Dex can replay a Step after Worker loss or before
a result is committed,
so the operations take these positions:

- **updateConversation** is safe to repeat. Its `PATCH` sets an absolute
  status and assignee and applies or removes tag IDs as sets, so a second
  dispatch leaves the conversation as one attempt would. It keeps async
  durability, and every unconfirmed outcome, including a 5xx, a lost response,
  or a read-back that does not show the change yet, is retried.
- **addNote** and **createSharedDraft** use the shared
  [single-dispatch fence](../../sdkgo/README.md#single-dispatch-fence) with sync
  durability. The fence reads the prior checkpoint before credentials. A
  missing, foreign, stale, or claimed marker on a later attempt forbids a
  second send. Only a `notSent` or `canResend` marker from the immediately
  preceding attempt permits another send. Credential failure and a pacer wait
  that cannot fit record `notSent` before Retry; definite 429, 503, and traced
  pre-connection failure record `canResend` before Retry.
- The pacer wait ends by half the initial attempt budget, before the fence's
  write deadline. With no note or draft read-back, the fence gives the write
  at most five sixths of that budget and reserves at least one tenth as
  minimum send time. An insufficient budget records `notSent` and retries.
- Hiver [documents 429 and 500 errors](https://developer.hiverhq.com/hiver-api)
  but says nothing about whether a 503 could follow an applied write. The
  connector retries 503 under the connector retry contract; duplicate effects
  after a 503 remain **unverified** with Hiver. A 408, other 5xx, 409, lost
  response, or unreadable, invalid, or oversized 2xx selects `uncertain` and
  is never resent. Hiver's published [shared draft](https://developer.hiverhq.com/hiver-api/conversations/update-conversation-in-the-inbox-copy)
  and [conversation read](https://developer.hiverhq.com/hiver-api/conversations/get-a-conversation-in-the-inbox)
  APIs offer no Call ID lookup for a draft or note.
- Both writes send `X-Dex-Call-ID` as a stable correlation value across retries.
  Hiver does not document this header as an idempotency key, so it does not
  make a repeated write safe.

Keep these two Steps sync so results are committed promptly.

Dex does not acknowledge heartbeat persistence when the Worker writes the
checkpoint. A later attempt with no stored marker therefore fails closed and
never resends, even if the first request did not leave. A lost `canResend`
marker can likewise produce `uncertain` for a request Hiver never applied.
Checkpoints written by v0.21.x and earlier are invalid to the shared fence:
an in-flight `addNote` or `createSharedDraft` retrying after a 429 selects
`uncertain` after the upgrade, even if Hiver never applied it. Reconcile those
calls before upgrading.
`TestSharedFenceRejectsStaleCanResendAfterLostClaim` covers the stale-marker
case, and `TestSharedFencePacerWaitEndsBeforeStepDeadline` covers the pre-send
checkpoint.

## Errors

Hiver's error bodies hold only message text, as `{"errors":[{"message":...}]}`
or `{"Message":...}`, so the connector never reads them: a Failure names only
the HTTP status, such as `Hiver rejected the request (HTTP 400)`.

| Response | Reads and updateConversation | addNote and createSharedDraft |
| --- | --- | --- |
| 400, 422 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 3xx or 4xx other than 404 and 429 on the read-back after an accepted `PATCH` | `invalidResponse`, keeping the status's kind | not applicable |
| 404 | `notFound` where declared, otherwise `providerRejected` | `notFound` |
| 409, other 4xx | `providerRejected` | `uncertain` for 409; `providerRejected` for other 4xx |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry after `Retry-After` when present, and hold the client's later requests | same |
| traced DNS, connect, or TLS failure before a connection | Retry | Retry with `canResend` checkpoint |
| cancelled wait for a slot | Retry | Retry with `notSent` checkpoint |
| 503 | Retry, after `Retry-After` when present | Retry with `canResend` checkpoint |
| 408, other 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input | `defect`, with no request | `defect`, with no request |
| unavailable connection credentials | `defect`, with no request | `notSent` checkpoint, then Retry |

Hiver documents IDs as JSON strings in some responses and numbers in others;
the connector accepts both and reports every ID as a string. The Receipt
carries the Call ID, the inbox, conversation, note, or draft ID, and an
`X-Request-Id` header when present, which Hiver does not document.

## Not in this release

### Not in Hiver's API

Sending a reply, creating a conversation, unassigning, reading message bodies,
subjects, requesters, timestamps, or SLA fields, listing notes, and filtering
the conversation list are not in Hiver's API documentation. Note mentions,
`notify_all`, threaded note replies, attachments, and the shared draft's
`reply_type` are documented but left out until their multipart encoding can be
verified against a live account.

## Example

[`examples/new-conversation-intake`](go/examples/new-conversation-intake)
starts one Flow per new conversation of the picked shared inbox through the
`conversationReceived` Trigger and reads its message IDs with
`getConversation`.

[`examples/claim-conversation`](go/examples/claim-conversation) is a runnable Dex
Web **Start Flow** example that uses all six operations: it finds a shared
inbox by its address, claims its first open unassigned conversation for a
Hiver user, applies a tag, adds one internal note, and leaves a shared reply
draft.

## Mocks

`hivermock` provides one consistent claim scenario: three inbox pages and
three conversation pages lead to an open unassigned conversation, then the
read, claim, note, and draft succeed. Each operation has one happy-path
default and mocks for its definite optional business outcomes. The defaults
are meant for the sample `support@acme.example.com` inbox and conversation
`573741352`; script other inputs explicitly. No operation takes a structured
output schema, so there are no mock request or answer checks.

The example's [real-Dex mock test](go/examples/claim-conversation/flow/workflow_mock_integration_test.go)
uses the package this way:

```go
mock := hivermock.New(t, ConnectionName)
harness := newClaimHarnessWithConnection(t, mock.Connection())
outcome := harness.runClaim(t, "mock", integrationClaimInput())
require.Equal(t, ClaimActionClaimed, outcome.Action)
require.Equal(t, 3, outcome.InboxPagesRead)
require.Equal(t, 3, outcome.ConversationPagesRead)
```

The test also asserts every operation's input, branch, and call count. It
requires a real Dex Server and runs in CI.

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
go run ./cmd/connectorctl validate connectors/hiver/connector.yaml
go run ./cmd/connectorctl generate --check connectors/hiver/connector.yaml
```

The `inboxPicker` Studio bundle builds and tests from `connectors/hiver/ui`:

```bash
npm ci --prefix sdk/react && npm run build --prefix sdk/react
cd connectors/hiver/ui && npm ci && npm test && npm run build
```

The provider fakes cover bearer authentication, request spacing across
concurrent Steps and a cancelled wait, the client-wide hold after a 429 and
its reset, page tokens and limits, string and numeric IDs, the documented
one-element conversation array, `close` read as `closed`, the assignee and
tag-name lookups and the five-page tag limit, the `PATCH` body and read-back,
a rejected read-back, multipart notes and drafts, the dispatch checkpoint and
its safe-retry marker, a checkpoint Dex never stored, status classification without
Hiver's message text, `Retry-After`, redirects, oversized, malformed, and
credential-reflecting responses, 503 retry with the same correlation Call ID,
missing heartbeat fail-closed behavior, and accepted responses without an ID. The `conversationReceived` fake
covers the baseline walk, newest-first and oldest-first lists, sweeps across
polls, deleted conversations, expired page tokens during the baseline and a
sweep, 429 with and without `Retry-After`, a missing inbox, invalid and
re-pointed cursors, and delivery across a restart on a real Dex Server.

No live Hiver account was used. The following live behavior is unverified:
the element type of `tags.to_apply` and `tags.to_remove`, which Hiver's
documentation leaves untyped and the connector sends as tag ID strings; the
status value Hiver returns for a closed conversation; whether the update
applies synchronously so that the read-back shows it; whether notes and
shared drafts accept the multipart fields the connector sends and how the
draft body renders markup; the order of `message_ids`; whether
`users/search` matches email exactly; whether list endpoints honor `limit` and
`next_page` for conversations and tags as the general pagination section
describes; whether 429 responses carry `Retry-After` and are always returned
before a write is applied; the `X-Request-Id` header; the Admin Panel path
in the setup guidance; for `conversationReceived`, the order of the
conversation list, how Hiver answers an expired `next_page` token, whether
the list includes closed conversations, and the inbox list fields the picker
reads.

## TypeScript package

The TypeScript package at [`typescript/`](typescript/README.md) implements all six operations in `@superdurable/connector-hiver` version `0.1.0`. Its checked-in list-inboxes Flow uses the static `hiver-support` connection and runs through a real Dex Worker and Client in the integration test. The Go-recorded cross-language scenarios cover every operation branch, 429 and 503 Retry, transport failure, multipart note and draft writes, and uncertain outcomes. It does not implement the `conversationReceived` Trigger. Live Hiver provider behavior is still unverified.
