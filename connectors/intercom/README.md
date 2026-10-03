# Intercom Connector

> **Verification status: partial live.** Only placeholder-token requests reached Intercom, and all returned 401; everything else ran on a real Dex stack against a local stand-in. No live Intercom workspace was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Intercom Connector reads and answers Intercom conversations from Dex Flows
and starts Flows from signed Intercom conversation webhooks. It exposes these
operation-specific Dex Step factories:

| Operation | Kind | Happy branch | Other branches |
| --- | --- | --- | --- |
| `intercom.NewSearchConversationsStep` | Query | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `intercom.NewGetConversationStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `intercom.NewReplyToConversationStep` | Mutation | `replied` | `notFound`, `providerRejected`, `uncertain`, `invalidResponse`, `defect` |
| `intercom.NewUpdateConversationStateStep` | Mutation | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `intercom.NewFindContactByEmailStep` | Query | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout. `replyToConversation` uses sync durability
and a five-attempt, five-minute retry window; the others use async durability
and a six-attempt, five-minute window, which fits Intercom's 10-second
rate-limit windows.

The `conversationEvent` Trigger receives Intercom conversation webhook
notifications, verified with `X-Hub-Signature`. The Studio bundle ships two
configuration units: `adminPicker`, which lists the workspace's admins, and
`conversationTopicPicker`, which selects a binding's webhook topics.

Every request sends `Intercom-Version: 2.16` (`intercom.APIVersion`), the
newest Intercom REST API version, released on 2026-07-15.

## Intercom setup

A connection needs these values:

- **region**, non-secret configuration: `us`, `eu`, or `au`, the workspace's
  data hosting region. It selects the API host: `https://api.intercom.io`,
  `https://api.eu.intercom.io`, or `https://api.au.intercom.io`. A workspace
  that opens on `app.eu.intercom.com` is `eu`, one on `app.au.intercom.com` is
  `au`. An access token works only on its region's host.
- **access_token**, a secret credential field: the access token of an Intercom
  app installed in the workspace, from the app's **Configure >
  Authentication** page in the
  [Developer Hub](https://app.intercom.com/a/apps/_/developer-hub). The
  connector sends it as an `Authorization: Bearer` token.
- **client_secret**, an optional secret credential field: the same app's
  client secret from **Configure > Basic Information**. Intercom signs webhook
  notifications with it. Only the `conversationEvent` Trigger uses it, and it is
  never sent to Intercom.

Intercom development workspaces are US-only, so an EU or AU workspace creates
the app in its paid workspace. Set the app's **API Version** menu to 2.16:
webhook notifications use the app's version, while API requests carry the
header. To rotate or revoke the token, open **Test & Publish > Your
Workspaces** and choose **Regenerate token** or **Uninstall app**.

Credentials are reread before every provider call, so a regenerated token takes
effect without a restart. The region and size limits are startup configuration.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "connectorId": "intercom",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/intercom",
  "moduleVersion": "v0.1.0",
  "provider": "intercom",
  "connectionName": "intercom-support-inbox",
  "configuration": {"region": "us"},
  "credentials": {"access_token": "...", "client_secret": "..."}
}
```

Load it with `localconfig.LoadFromEnvironment` and
`intercom.NewLocalConnection`. The conversationEvent Trigger uses
`intercom.NewLocalConversationEventEndpointRunner`, which loads the connection
and each route's stored binding, wraps each target in a durable inbox, and
returns one `http.Handler` to mount. The checked-in example wires it like this,
from [`examples/answer-duplicate-conversation/main.go`](examples/answer-duplicate-conversation/main.go):

```go
func newInboundEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *answerduplicate.Flow, logger *slog.Logger, connectionOptions []intercom.Option,
) (*intercom.ConversationEventEndpointRunner, error) {
	bindingLogger := logger.With("connector", intercom.ConnectorID, "connection", answerduplicate.ConnectionName,
		"trigger", intercom.ConversationEventTriggerDefinition.Trigger.TriggerName, "binding", answerduplicate.InboundTriggerBinding)
	return intercom.NewLocalConversationEventEndpointRunner(store, answerduplicate.ConnectionName, []intercom.LocalConversationEventTriggerRoute{{
		BindingName: answerduplicate.InboundTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, answerduplicate.AcceptInboundConversation, answerduplicate.ResolveFlowID,
			answerduplicate.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, connectionOptions...)
}
```

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
`access_token` and an optional `client_secret` and rejects anything else
without repeating either value.

## States

The connector takes and returns Intercom's own values and never maps them to
another vocabulary:

- conversation `state`: `open`, `closed`, `snoozed`
  (`intercom.ConversationStates()`). `open` is a conversation in an inbox,
  `closed` one a teammate closed, which a customer reply reopens, and `snoozed`
  one hidden until `snoozedUntil`, when Intercom reopens it. Intercom's `open`
  flag (`Conversation.IsOpen`) is true for open and snoozed conversations.
- conversation `priority`: in API version 2.16, `none`, `low`, `medium`,
  `high`, or `urgent`; versions up to 2.15 report `priority` or `not_priority`.
  Results pass the value through.
- Intercom tickets are a separate API with their own vocabulary: a ticket state
  belongs to one of the categories `submitted`, `in_progress`,
  `waiting_on_customer`, and `resolved`, and each workspace defines its own
  ticket states, with internal and external labels, inside those categories.
  This release has no ticket operations; see
  [Not in this release](#not-in-this-release).

Inputs are validated against the documented values before any request.

## Operations

### searchConversations

`searchConversations` builds one query for `POST /conversations/search` from
typed filters:

- `states`: any of the given states, an OR group of `state =` filters.
- `contactEmail`: `source.author.email =`, the email of whoever sent the
  first message. An admin-initiated conversation's source author is the admin,
  so use `contactIds` to find every conversation with a contact.
- `contactIds`: any of up to 15 contact IDs, an OR group of `contact_ids =`.
- `tagIds`: any of up to 15 tag IDs, an OR group of `tag_ids =`. Intercom
  searches tags by ID, not by name.
- `updatedSince`: an RFC 3339 instant with an explicit offset, sent as
  `updated_at >` its Unix time. Intercom documents `>` on conversation
  timestamps as greater than or equal.

At least one filter is required, and filters combine in one AND group, within
Intercom's limits of two nesting levels and 15 filters per group.
`BuildConversationSearchQuery` returns the query for review. `pageSize` is 1 to
150, 20 by default, and `nextCursor` reads the next page with
`starting_after`. Search results omit conversation parts and the first
message's body, so pages stay small, and Intercom's index can lag a change by a
short time, so search is not a safe duplicate check immediately after a write.

### getConversation

`getConversation` reads `GET /conversations/{id}?display_as=plaintext`, which
returns the conversation with up to its 500 most recent parts as plain text,
and keeps the newest `latestPartLimit` of them, 1 to 50 and 10 by default,
newest first. `hasOlderParts` and `partCount` report what was left out. Parts
include replies (`comment`), internal notes (`note`), and events such as
`close`, `open`, `snoozed`, and `assignment`, each with its author. The first
message is `conversation.source.body`. Every body is cut at 16 KiB
(`intercom.MaxTextBytes`) on a UTF-8 boundary and flagged `isBodyTruncated`,
so Step state stays bounded. A missing conversation selects `notFound`.

### replyToConversation

`replyToConversation` sends `POST /conversations/{id}/reply` with
`type: admin`, the admin, and `messageType` `comment` (the customer sees it)
or `note` (only teammates see it). `messageType` is required, so a
customer-visible message is never a default. The plain-text body is escaped
into HTML paragraphs, so markup is shown literally and line breaks are kept.

Intercom documents no idempotency key for replies, and a repeated `comment` is
a second message to the customer, so the operation never sends twice in one
Step execution:

1. It runs with sync durability, so Dex does not dispatch a second attempt
   while the first is in flight, as async durability does after about seven
   seconds.
2. Before sending, it records a Dex heartbeat checkpoint with the Worker's
   time. If Dex does not record it, nothing is sent and the attempt retries.
3. A 429 or a connection that never opened clears the checkpoint and retries,
   because Intercom did not apply the request.
4. A lost connection, an unreadable response, a 408, or a 5xx keeps the
   checkpoint and retries. Every later attempt, including one on a new Worker
   after the first was lost, finds the checkpoint and never sends. It reads
   the conversation and selects `replied` with `wasAlreadyApplied: true` when a
   part by the same admin, of the same type, with the same words, was created
   at most five minutes before the checkpoint or later. Otherwise it selects
   `uncertain`: the reply may exist, and the Flow must decide, for example by
   asking a teammate.
5. A conclusive 4xx selects `notFound` or `providerRejected`; a 2xx whose
   conversation cannot be decoded selects `invalidResponse`, and the reply
   exists.

`Part` is the new part as Intercom's response shows it.

### updateConversationState

`updateConversationState` closes, reopens, or snoozes a conversation as an
admin, with `POST /conversations/{id}/parts` and `message_type` `close`,
`open`, or `snoozed` with `snoozed_until`. Every attempt reads the conversation
first and writes only when its state differs; a snooze also compares
`snoozedUntil` in whole seconds. A conversation already in the requested state
selects `updated` with `wasAlreadyApplied: true` and nothing is written.

The operation is safe to repeat, so it keeps async durability and has no
`uncertain` branch: every unconfirmed write retries, and the retry reads
first. Two concurrent attempts, such as Dex's second dispatch of a slow Step,
can both write the same state. A close without a message sends the customer
nothing, so the only trace is a second close event in the conversation. When
Intercom rejects a write with a 4xx other than 401, 403, 404, or 429, the
operation reads again and selects `updated` with `wasAlreadyApplied: true` if
the conversation reached the requested state anyway. A missing conversation is
`notFound`.

### findContactByEmail

`findContactByEmail` reads one page of `POST /contacts/search` with
`email =`, at most `contactLimit`, 1 to 50 and 10 by default, and
`hasMoreContacts` reports more. Intercom can hold several contacts for one
email, such as a lead and a user for the same person, so `found` returns them
all and the Flow decides. Merged contacts are excluded, and Intercom can take a
few minutes to index a new contact, which selects `notFound` meanwhile.

## conversationEvent Trigger

The connection's client keeps one `webhooktrigger.Endpoint` per connection.
`Connection.ConversationEventWebhookHandler` returns it as an `http.Handler`,
and every `conversationEvent` binding built from the connection is a source of
that endpoint. For each request the handler:

1. answers `HEAD` with `200`, because Intercom validates a webhook URL with a
   HEAD request when it is saved;
2. accepts only `POST` and reads at most `webhookMaxBodyBytes`, answering
   `405` or `413` otherwise;
3. verifies `X-Hub-Signature`: `sha1=` and the hex HMAC-SHA1 of the raw body,
   keyed with `client_secret`, compared in constant time. A failed check
   answers `400`; a blank `client_secret` answers `503`;
4. decodes the notification. A topic other than the 14 conversation topics in
   `intercom.ConversationEventTopics()`, including Intercom's periodic
   `ping`, is answered `200` and dropped;
5. records the event in the durable inbox of every binding whose `topics`
   accept it, answers `200` only after every record is on disk, and then
   delivers it to each binding's target in arrival order.

The endpoint answers `503` while no binding runs, while it replays inboxes
after a restart, when a record fails, and when a binding's queue is full.
Intercom retries a failed notification after one minute, retries once, and
pauses a subscription after 1,000 consecutive errors in 15 minutes, so start
the runner as soon as the process starts. Responses never carry verification
or decoding detail.

The event ID is Intercom's notification `id`, which a redelivery reuses.
Intercom redelivers a notification that is not answered within five seconds,
and `sdkgo.NewDexFlowTriggerTarget` uses the event ID as the Flow-start request
ID, so a redelivery starts no second Flow. Intercom does not guarantee delivery
order; compare `notifiedAt`.

`ConversationEvent` carries the notification ID, topic, `workspaceId`
(Intercom's `app_id`), `notifiedAt`, and the conversation without its parts or
first-message body. An app installed in several workspaces sends every
workspace's notifications to one URL, so filter on `workspaceId` when that
matters. Intercom signs notifications without a timestamp, so a captured
notification can be replayed; the durable inbox and the Flow-start request ID
turn a replay into a duplicate.

`ConversationEventTriggerConfiguration.topics` selects a binding's topics; an
empty list accepts all 14. Intercom sends only the topics subscribed on the
app's **Configure > Webhooks** page. Provider configuration is not the
application's admission boundary; the example's `AcceptInboundConversation`
filter also admits only `conversation.user.created`.

## Studio units

- `adminPicker` lists the workspace's admins with `GET /admins` and saves one
  admin's numeric ID to its `adminId` port. Studio commands have a fixed host
  and an access token works only on its region's host, so the manifest
  declares one command per region, `listAdmins`, `listAdminsEU`, and
  `listAdminsAU`. When the host reports the connection's configuration, the
  bundle asks only its region; Dex Web `cli-v1.1.0` does not report it, so the
  bundle tries the US, EU, and AU hosts in order and the token reaches each
  Intercom host until one accepts it. A manual admin ID fallback stays
  available.
- `conversationTopicPicker` saves a binding's topics to its `topics` port from
  a fixed checklist of the supported conversation topics; it calls no
  provider.

Both units render inside the operation or Trigger configuration that a Flow
composes; the connection form has no unit fields. Credentials never reach the
bundle.

## Errors

A non-2xx response never exposes Intercom's error `message` text. A Failure
repeats only up to three error `code` tokens and their `field` names, such as
`Intercom rejected the request (HTTP 422) [parameter_invalid; fields: admin_id]`,
and the Receipt carries the error body's `request_id`.

| Response | Result |
| --- | --- |
| 400, 422 | `providerRejected`, `VALIDATION` |
| 401 | `providerRejected`, `AUTHENTICATION`, with a hint to check the region |
| 403 | `providerRejected`, `AUTHORIZATION` |
| 402 (`api_plan_restricted`), other 4xx | `providerRejected`, `PROVIDER_REJECTION` |
| 409 | `providerRejected`, `CONFLICT` |
| 404, 410 | `notFound` where declared, otherwise `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 429 | Retry after `X-RateLimit-Reset`, at most one minute |
| 408, 5xx, transport failure | Retry; `replyToConversation` reconciles first |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` |
| invalid input or connection credentials | `defect`, with no request |

## Not in this release

### OAuth

Intercom OAuth cannot be declared as a Dex Web authorization method:

- the manifest schema requires at least one scope, and Dex Web accepts the
  token exchange only when every declared scope appears in the response's
  `scope` string. Intercom permissions are chosen in the Developer Hub, not
  requested as scopes, and its token response is only `token_type`, `token`,
  and `access_token`, so every authorization would fail with
  `CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`;
- the authorization endpoint is regional (`app.intercom.com`,
  `app.eu.intercom.com`, `app.au.intercom.com`). Intercom documents that the
  US host lets an EU or AU customer pick their region, except when they sign in
  with Google, but the manifest's static endpoint cannot follow the region
  configuration;
- Intercom documents the token exchange only at
  `https://api.intercom.io/auth/eagle/token`, so the exchange for an EU or AU
  workspace is unverified.

`sdkgo/oauthtoken` is not needed for Intercom: its tokens carry no
`expires_in` and there is no refresh token, so nothing would be refreshed.

### Tickets and teams

Ticket operations, a team picker, and conversation assignment are not in this
release; `GET /teams` returns a JSON object, so a team picker can follow the
admin picker's pattern.

## Example

[`examples/answer-duplicate-conversation`](examples/answer-duplicate-conversation)
starts one Flow per new conversation from the `conversationEvent` Trigger,
uses all five operations, and answers and closes the conversation only when it
duplicates the customer's earlier open conversation.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
(cd ui && npm ci && npm test && npm run build)
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, Trigger, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/intercom/connector.yaml
go run ./cmd/connectorctl generate --check connectors/intercom/connector.yaml
```

The provider fakes cover the bearer token and version header, regional hosts,
query building and filter limits, cursor pagination, newest-first parts and
truncation, earlier API version payload shapes, the reply checkpoint and its
reconciliation, state comparison and rejected-write reconciliation, contact
search, webhook signatures, topics, HEAD validation, Intercom error codes
without message text, `X-RateLimit-Reset`, redirects, and oversized,
malformed, and credential-reflecting responses.

No live Intercom workspace was used. Requests with a placeholder token to
`GET /admins` on all three regional hosts, and the example's
`getConversation` on the EU host, answered HTTP 401 with an `error.list` whose
code is `unauthorized` and a `request_id`, which the connector maps to
`providerRejected`. The following live behavior is unverified: every
response to a valid token; the response bodies of `POST /conversations/{id}/reply` and
`POST /conversations/{id}/parts`; how `display_as=plaintext` renders a reply
sent as escaped HTML paragraphs, which reconciliation compares word by word;
how long a new reply takes to appear in `GET /conversations/{id}`; whether
closing or snoozing a conversation that is already in that state answers 2xx or
4xx; whether `updated_at >` includes the instant; whether `source.author.email`
and `email =` match case-insensitively; the author type Intercom reports for a
contact who starts a conversation, which the example accepts as `user`, `lead`,
or `contact`; whether a valid access token on another region's host answers
401; the `X-RateLimit-Reset` header on a 429; the HEAD
validation and `X-Hub-Signature` on real notifications; whether a redelivered
notification keeps its `id`; and the Developer Hub paths in the setup
guidance.
