# Hiver Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack
> against a local stand-in for Hiver; no live Hiver account was used. See
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
which may make up to eight spaced requests, uses 60 seconds. Every retry window
is at least five minutes. `addNote` and `createSharedDraft` use sync durability
because Hiver has no idempotency key; see [Duplicate safety](#duplicate-safety).

This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Hiver setup

A connection needs one secret and has two optional settings:

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

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/claim-conversation/main.go`](examples/claim-conversation/main.go)
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
the connector cannot detect. Dex re-dispatches an async Step whose local
attempt passes about seven seconds, and it retries a Step after a lost Worker,
so the operations take these positions:

- **updateConversation** is safe to repeat. Its `PATCH` sets an absolute
  status and assignee and applies or removes tag IDs as sets, so a second
  dispatch leaves the conversation as one attempt would. It keeps async
  durability, and every unconfirmed outcome, including a 5xx, a lost response,
  or a read-back that does not show the change yet, is retried.
- **addNote** and **createSharedDraft** run with sync durability, so Dex never
  sends a second attempt while the first is in flight. Before sending, the
  operation records a Dex heartbeat checkpoint naming the Step's Call ID. A
  later attempt of the same Step execution that finds the checkpoint, after a
  lost Worker or an Execute timeout, selects `uncertain` without sending and
  without waiting for a request slot.
- Only an outcome that proves Hiver did not apply the request is retried: a
  429, a connection that failed before it opened, and a cancelled wait for a
  request slot, which happens before the checkpoint. The checkpoint is cleared
  first.
- A 5xx, a 408, a lost or unreadable response, and an invalid or oversized
  2xx select `uncertain`: the note or draft may exist, and the Step does not
  resend it. The application decides, for example by asking a person to check
  Hiver, as the example does.

Keep these two Steps sync. An application override to async durability lets
Dex dispatch a second request after seven seconds.

The checkpoint narrows the duplicate window but does not close it. Dex accepts
the checkpoint when the Worker writes it to its stream, before the request
leaves; a Worker that loses its Dex connection in that instant, before Dex
stored the checkpoint, could still send a second note or draft when Dex
retries the Step. `TestDispatchCheckpointThatDexNeverStoredIsSentAgain`
documents that case. The other direction is safe: a crash after the
checkpoint but before the request left, or a cleared checkpoint after a 429
that Dex never stored, reports `uncertain` for a request Hiver never applied.

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
| 409, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 | Retry after `Retry-After` when present, and hold the client's later requests | same |
| connection refused before sending, or a cancelled wait for a slot | Retry | Retry |
| 408, 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input or connection credentials | `defect`, with no request | `defect`, with no request |

Hiver documents IDs as JSON strings in some responses and numbers in others;
the connector accepts both and reports every ID as a string. The Receipt
carries the Call ID, the inbox, conversation, note, or draft ID, and an
`X-Request-Id` header when present, which Hiver does not document.

## Not in this release

### Triggers

Hiver's API documentation describes no webhooks or event subscriptions, and
`sdkgo` has no durable per-binding poll cursor for a polling Trigger, so the
connector declares no Trigger. A Flow that reacts to new conversations polls
`listConversations` on a Timer.

### Studio pickers

An inbox picker would fit the Studio setup command limits: `GET
https://api2.hiverhq.com/v1/inboxes` is a fixed HTTPS host with a bearer
credential and a JSON object response. This release keeps the inbox an
operation input instead, and the example resolves it from the shared mailbox
address a person already knows through `listInboxes`.

### Not in Hiver's API

Sending a reply, creating a conversation, unassigning, reading message bodies,
subjects, requesters, timestamps, or SLA fields, listing notes, and filtering
the conversation list are not in Hiver's API documentation. Note mentions,
`notify_all`, threaded note replies, attachments, and the shared draft's
`reply_type` are documented but left out until their multipart encoding can be
verified against a live account.

## Example

[`examples/claim-conversation`](examples/claim-conversation) is a runnable Dex
Web **Start Flow** example that uses all six operations: it finds a shared
inbox by its address, claims its first open unassigned conversation for a
Hiver user, applies a tag, adds one internal note, and leaves a shared reply
draft.

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

The provider fakes cover bearer authentication, request spacing across
concurrent Steps and a cancelled wait, the client-wide hold after a 429 and
its reset, page tokens and limits, string and numeric IDs, the documented
one-element conversation array, `close` read as `closed`, the assignee and
tag-name lookups and the five-page tag limit, the `PATCH` body and read-back,
a rejected read-back, multipart notes and drafts, the dispatch checkpoint and
its clearing, a checkpoint Dex never stored, status classification without
Hiver's message text, `Retry-After`, redirects, and oversized, malformed, and
credential-reflecting responses.

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
before a write is applied; the `X-Request-Id` header; and the Admin Panel path
in the setup guidance.
