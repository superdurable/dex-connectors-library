# Slack Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/slack/go@v1.7.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/slack`.

This module finds Slack channels, identifies the connected workspace, reads
recent channel history, reads and writes Slack channel threads, and receives Slack Events
API messages through Socket Mode, or from a host that relays them for a shared
connection (see [Relayed events](#relayed-events)). In Go it also posts
messages with Block Kit buttons and receives their clicks through Socket Mode
(see [Buttons](#buttons)). It supports public and private channels, not direct
messages.

## Agent tools

The Go and TypeScript packages offer all seven operations to Dex agent hosts,
and the Python package offers the six other than `listChannelMessages`. Both
posts recommend approval and claim no safe retry, so a host runs each post
once. The reads need no approval:

- `slack_list_channels` (`listChannels`) finds channel IDs from a name such as
  `#general`. The post tools tell the model to look a channel up with it
  instead of asking the user for a raw channel ID.
- `slack_get_workspace_identity` (`getWorkspaceIdentity`) names the workspace
  and bot that the connection posts as.
- `slack_list_channel_messages` (`listChannelMessages`) reads one page of a
  channel's recent top-level messages.

Neither post resolves channel names itself; it still takes an exact channel ID.

From Go v1.4.0, `Connection.AgentTools()` binds the exposed operations to a
connection as `sdkgo.AgentTool` values, and `ExposedAgentTools()` lists their
metadata; v1.5.0 adds the two discovery reads, and v1.6.0 adds
`listChannelMessages`. A `slackmock` connection returns
the same tools for host tests.

## TypeScript package

`@superdurable/connector-slack@0.5.0` implements the channel discovery,
channel history and thread reads, and message post operations for TypeScript Dex Flows. Socket Mode Triggers remain
Go-only. The [runnable thread page example](typescript/examples/list-thread-messages/README.md)
uses the static `slack-thread-reader` connection name, and the
[channel history example](typescript/examples/list-channel-messages/README.md)
uses `slack-channel-history-reader`; both obtain OAuth bot and user tokens
through Dex project credentials. The TypeScript package uses
generated manifest refresh declarations for token rotation. Live Slack OAuth
and provider behavior have not been verified for this package.

## Slack app setup

1. Create a Slack app and enable Socket Mode.
2. Add bot scopes `channels:history`, `groups:history`, `channels:read`,
   `groups:read`, `users:read`, and `chat:write`.
3. Add user scopes `channels:history` and `groups:history`. Slack requires a
   user token for `conversations.replies` on public and private channels.
4. Subscribe to the `message.channels` and `message.groups` bot events.
5. Install the app, then create an app-level token with `connections:write`.
6. Invite the bot to every private channel it must observe or post to.
7. To receive button clicks, open **Interactivity & Shortcuts** and turn
   Interactivity on. With Socket Mode enabled Slack asks for no Request URL,
   and clicks need no additional scope.

Dex Web stores the OAuth bot and user tokens. When Token Rotation is enabled,
it also stores both one-use refresh tokens, and the connector refreshes the two
12-hour access tokens together within five minutes of their recorded expiry.
Each replacement pair is persisted atomically in project storage; an
`invalid_refresh_token` response marks the connection for reauthorization. Dex
Web also records the access-token expiry that refresh requires; do not
hand-author rotating-token metadata. Enter the
`xapp-` app-level token in the host-owned secret field; it is never sent to the
Studio iframe. The `app_token` setting is optional: a connection without it
receives its events from a host that relays them instead of through Socket
Mode. Use Slack's standard OAuth flow with the host-owned client
secret. Slack's localhost PKCE installation mode cannot request bot scopes.

## Trigger settings

Dex Web shows searchable channel and member pickers. It stores stable channel
IDs such as `C0123456789` and member IDs such as `U0123456789`; names and
avatars are display-only.

`threadTriggerMatcher.messageContains` is optional. Empty text matches every
human top-level message from an allowed poster. `posterUserIds` is also
optional for the root trigger; an empty list means any human poster.

`threadReplyMatcher.posterUserIds` must contain at least one approver. Text
matching is case-insensitive substring matching. For example, `approve` also
matches `disapprove`; use a more distinctive phrase if that is undesirable.

The application owns the mapping from Slack events to Flow IDs. Pass the same
pure `FlowIDResolver` to both targets so root and reply events resolve to the
same Flow. Prefer a readable ID built from team ID, channel ID, and root
timestamp. RPC registration is code, not Trigger configuration. Pass the same
direct bound Flow method to `dex.DefineRPC` and `NewDexRPCTriggerTarget`. Use an
`RPCInputMapper` to convert the provider event into an application-owned RPC
input. The application owns RPC options, durable state, locking, and event
deduplication.

Both Dex targets also require an application-owned `TriggerFilter`. It
runs before Flow ID resolution and is the final admission rule for starting a
Flow or invoking an RPC. Provider matchers reduce Socket Mode traffic, while
the application filter can independently enforce channel, poster, message, or
other domain rules. Returning false consumes the event without resolving a
Flow ID, mapping input, or calling Dex. Filters, resolvers, and mappers are
deterministic, side-effect-free functions without error results.

If a picker cannot load, copy IDs manually:

- Open channel details in Slack and choose **Copy channel ID**.
- Open a member profile, open its menu, and choose **Copy member ID**.

## Project configuration

Dex Web or a hosting platform saves the connection, its credentials, and both
Trigger bindings in the project configuration. The application loads that
configuration once and opens the connection by name, as
[`examples/thread-approval/main.go`](go/examples/thread-approval/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := slack.NewProjectConnection(project, threadapproval.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Then create one
`NewProjectMessageTriggerRunner` containing every Slack message Trigger route
for the connection. Slack distributes Socket Mode events among active WebSocket
connections, so separate root and reply runners must not compete for the same
workspace events. The shared runner reads each route's binding from the project
configuration and keeps a separate configuration, target, and durable inbox for
each binding while receiving every message through one socket. The inboxes are
kept in project storage.

Socket Mode envelopes are persisted in every matching binding-specific inbox
and acknowledged before their targets run. Events reach Dex in the order the
runner receives them, so a root message is delivered before any reply or button
click received after it. If a connection drops, the runner delivers the events
it had persisted but not yet delivered before it reads envelopes from the next
connection. Slack does not guarantee event order, so a reply that Slack sends
before its root is consumed as undeliverable. The runner hands each event to
`sdkgo.DeliverTrigger`:

- An undeliverable event is consumed. Examples are a reply in a thread whose
  Flow finished, failed, or never started. One stray reply therefore cannot
  block the other bindings.
- Any other failure, such as a Dex or Worker outage, retries with backoff from
  250 milliseconds up to 30 seconds.

How a retry affects later envelopes depends on the targets:

- When every route's target is a durable inbox, as with
  `NewProjectMessageTriggerRunner` or `provider.NewDurableTriggerTarget`
  targets, one delivery goroutine per connection delivers the acknowledged
  events while the socket reader keeps recording and acknowledging later
  envelopes. A retrying delivery therefore does not delay another binding's
  acknowledgement, which Slack requires within three seconds for a button
  click. The delivery queue holds 256 envelopes; when it is full, the reader
  holds the envelope it just acknowledged and pauses until one is delivered, so
  up to 258 acknowledged envelopes await delivery, counting the one being
  delivered. Paused envelopes are not yet
  acknowledged, so Slack resends them, and every acknowledged event is already
  in its inbox. The runner finishes delivering the waiting events before it
  reconnects. A durable inbox holds its lock during
  one delivery attempt, so the same binding records its next event only after
  that attempt returns.
- Otherwise, such as a generated per-Trigger factory or `NewMessageTriggerRunner`
  with plain targets, delivery stays inline on the socket reader. While an
  event retries, the runner does not read later envelopes, so Slack may
  redeliver them, and at most one acknowledged event awaits delivery.

At startup and before every reconnect, the runner replays pending events in
order, roots first, with the same backoff, and only then opens the socket.
Replay never ends the process because of one event. Flow starts deduplicate by
event ID, and an approval RPC must also treat a repeated event ID as a
duplicate, so a crash at either side of inbox cleanup remains safe.

Slack's servers ping every Socket Mode connection. The runner answers each ping
and treats it as proof that the connection is alive, so a quiet channel keeps
one connection open. A connection that receives neither an envelope nor a ping
for 45 seconds is closed and replaced. When Slack sends a `disconnect`
envelope, such as `refresh_requested` every few hours, the runner reconnects
after one second. A failed connection attempt is retried after one second,
doubling up to 30 seconds while attempts keep failing. The delay starts again
at one second once a connection receives Slack's `hello`. Canceling the
runner's context closes the socket at once.

## Buttons

`PostButtonMessage` (Go only) posts one message whose text appears in a Block
Kit section above 1 through 25 buttons, as a top-level message or, with
`threadTimestamp`, as a thread reply. Each `MessageButton` has a required
`ActionID`, unique in the message and at most 255 characters; a required
plain-text `Text` label of at most 75 characters; an optional `Value` of at
most 2000 characters that the click returns unchanged; and an optional `Style`
of `ButtonStylePrimary` or `ButtonStyleDanger`. The message text is also the
notification fallback and must be at most 3000 characters, Slack's section
limit. Invalid input selects `defect` before anything is sent. The post uses
`chat.postMessage` with the existing `chat:write` scope and the same
single-dispatch fence, retry policy, read-back, and branches as
`PostThreadReply`. Treat a button value like message text: every member who
can see the message can read it, and a click delivers it to the Flow and
records it in the binding's durable inbox, so it must never contain a secret or
token.

The `buttonClicked` Trigger delivers a `ButtonClickEvent` for each click on a
button in its channel: team, channel, message timestamp, thread timestamp (the
message's own timestamp for a top-level message, as `MessageEvent` reports a
root), clicker user ID, action ID, value, and action timestamp. Slack sends
clicks as `block_actions` interactive payloads over the same Socket Mode
connection as messages, so add `ButtonClickedRoutes` to the one
`NewProjectMessageTriggerRunner` of the connection; Slack sends each event to
only one of an app's connections. A click is acknowledged before it is
delivered, so button routes require durable inboxes: `NewMessageTriggerRunner`
with button routes returns an error unless every route's target, message
routes included, comes from `provider.NewDurableTriggerTarget`, and `Run` of
the generated `NewButtonClickedTrigger`, whose runner cannot pass a durable
inbox to its source, always returns an error. Its configuration takes the
required `channelId`, optional `actionIds` to receive only those buttons, and
optional `clickerUserIds` to receive only clicks by those members; an empty
list matches everyone. Clicks on views, modals, and other block elements are
acknowledged and ignored.

An interactive payload has no event ID, so the event ID joins the team,
channel, message timestamp, action ID, and action timestamp: each click is a
separate event, and a replayed click deduplicates. An approval RPC should
accept the first decision and treat later clicks as no-ops, as
[`examples/button-approval`](go/examples/button-approval) does. The runner
acknowledges each click as soon as its durable inbox recorded it and logs a
WARN `slack interactive envelope acknowledged late` record when recording took
longer than Slack's three seconds; Slack has then shown the clicker an error,
but the recorded click is still delivered. Events and log records never carry
the payload's response URL, trigger ID, or verification token, and log records
never carry the button value.

Clicks reach only Socket Mode: a relaying host forwards Events API messages,
not interactive payloads. On a connection without an `app_token`, `Run` of a
runner with button routes returns an error.

## Relayed events

A host that installs its own Slack app in many workspaces cannot hand each
application that app's `xapp-` token, because one Socket Mode connection would
receive every workspace's events. It receives the Events API deliveries over
HTTP instead and relays each event to the applications whose shared connection
serves that workspace. The connection's credential then has no `app_token`.

`NewProjectMessageTriggerRunner` decides when `Run` starts. It reads the
connection's credential, retrying with the Socket Mode backoff while it is
unavailable. With an `app_token` it uses Socket Mode as described above.
Without one, on a shared connection (an entry with `sharedConnectionId`), it
receives relayed events; on any other connection `Run` returns an error.
Mount the runner on the application's HTTP server at
`slack.RelayedTriggerPath(connectionName)`, which is `/triggers/slack/`
followed by the connection name in unpadded base64url, as
[`examples/thread-approval/main.go`](go/examples/thread-approval/main.go) does:

```go
mux := http.NewServeMux()
mux.Handle(slack.RelayedTriggerPath(threadapproval.ConnectionName), triggerRunner)
```

In relayed mode `Run` replays every binding's durable inbox, starts receiving,
and then registers the connection name with the host through
`projectconfig.LoadedProject.RegisterRelayedTrigger`, so the host knows where
the connection's events go. It retries a host that is unavailable and returns
an error when the host does not relay the connection, when the scope no longer
selects it, or when the host refuses the registration. It registers again on
every start.

Each relayed request carries Slack's `event_id`, `event_time`, outer `team_id`
and inner `event`, the shape of a Socket Mode payload, signed with the scope
token in `Dex-Relay-Signature`. The runner checks the signature with
`projectconfig.LoadedProject.VerifyRelayedRequest` and answers:

- `200` once every binding that matches the message recorded it in its durable
  inbox, or at once for a message no binding matches;
- `400` for a missing, wrong or stale signature, or a body that is not a Slack
  callback;
- `413` for a body over 1 MiB;
- `503` while the runner does not receive relayed events: before `Run`, while
  it replays the inboxes, and for a connection that uses Socket Mode.

After answering, the runner delivers each event to its routes inline in arrival
order, as Socket Mode does, so a root is delivered before any reply received
after it. A host that relays an event once does not redeliver it, so an event
is durable only once the runner answered `200`. Slack's `event_id` is the
Trigger event ID, so Dex starts one Flow per event even when the host or Slack
delivers it again; an RPC target still treats a repeated event ID as a
duplicate.

## Logging

The runner logs through `log/slog`, to `slog.Default()` unless you pass
`slack.WithLogger(logger)` to `NewProjectMessageTriggerRunner` or `New`. The
logger also reaches the durable inboxes that `NewProjectMessageTriggerRunner`
creates.

| Level | Message | Attributes |
| --- | --- | --- |
| INFO | `slack socket mode connected` | `reconnect` |
| INFO | `slack socket mode disconnected; reconnecting` | `reason`, `delay` |
| WARN | `slack socket mode connection failed; reconnecting` | `attempt`, `delay`, `error` |
| DEBUG | `trigger event ignored` | `event_id`, `channel`, `reason`, and `subtype` for a message subtype |
| DEBUG | `slack socket mode envelope ignored` | `type`, `envelope_id` |
| WARN | `trigger event skipped: undecodable` | `envelope_id`, `error` |
| INFO | `slack relayed trigger registered` | |
| WARN | `slack relayed trigger registration failed; retrying` | `attempt`, `delay`, `error` |
| WARN | `slack relayed request refused` | `reason`: `verification_unavailable` |
| DEBUG | `slack relayed request refused` | `reason`: `invalid_signature` |
| WARN | `slack trigger credentials unavailable; retrying` | `attempt`, `delay` |

Relayed records never contain a header value, a signature or the body. A
`verification_unavailable` refusal means the scope token could not be read,
and the host is answered `503`. A request with a missing, wrong or stale
signature is logged only at DEBUG, because the route is public and anyone can
send one.

Every record carries `connector` and `connection`; route records also carry
`trigger` and `binding`. A healthy connection logs one INFO
`slack socket mode connected` record and nothing else until it ends. Slack's
routine refreshes log INFO `slack socket mode disconnected; reconnecting` with
Slack's `reason`, such as `refresh_requested`, `warning`, or `link_disabled`.
Only a failure logs WARN: a missing credential, a rejected
`apps.connections.open` call, a dial error, or a socket that dropped or went
silent. `attempt` counts the connection attempts that failed in a row, and
`delay` is the wait before the next one. When Slack rejects the connection, the
`error` names Slack's error code and the HTTP status, for example
`Slack rejected the Socket Mode connection: invalid_auth (HTTP 200)`; a value
that is not a Slack error code is reported as `unknown`. The `reason` of an
ignored message is one of `missing_event_id`, `not_a_message`, `subtype`,
`bot`, `missing_user`, `channel_mismatch`, `not_a_reply`, `not_a_root`, or
`matcher_mismatch`. Each route logs its own reason, so a root message also
appears as `not_a_reply` for the reply route. Ignored messages are DEBUG
because most channel traffic is ignored.

The runner also emits the `sdkgo` delivery records described in the SDK
README, such as `trigger event skipped: undeliverable` and `trigger delivery
failed; retrying` with each backoff delay. For a live event, those records,
including the durable inbox's and the Dex targets', carry the message's
`channel` and `thread_ts`, so one `thread_ts` finds every record about a
thread. Events that the inbox replays after a restart or reconnect carry only
their `event_id`. Records never contain message text, tokens, or the Socket
Mode URL, whose connection ticket is removed from dial errors.

The generated per-Trigger factories, such as `NewThreadReplyCreatedTrigger`,
pass `WithLogger` only to the Socket Mode source, and their source records carry
no `binding`. Their runner records go to `slog.Default()`, so call
`slog.SetDefault` when you use them, or use `NewProjectMessageTriggerRunner`.

## Operations

- `ListChannels` returns one page of non-archived public and private
  channels with their IDs, names, privacy, app membership, topic, and purpose.
  It calls `conversations.list`, or `users.conversations` when `memberOnly` is
  true, with the bot token and the existing `channels:read` and `groups:read`
  scopes. `limit` (1–999, default 200) is Slack's page size; `nameFilter`
  keeps names containing the text, ignoring case and one leading `#`, after
  Slack returns the page, so a page may be empty while `nextCursor` continues
  the listing. `visibility` selects `public` or `private` channels. Slack lists
  a private channel only after the app is added to it.
- `GetWorkspaceIdentity` sends a form-encoded POST to `auth.test`, as Slack
  documents it, with the bot token. It returns the workspace ID, name, and
  URL, the Enterprise Grid ID when present, and the bot user ID, user name,
  and bot ID. It needs no scope.
- `ListThreadMessages` returns one page and a next cursor. Page size is 1–15.
- `ListChannelMessages` reads one page of a channel's top-level messages from
  `conversations.history`, newest first, with the bot token and the existing
  bot `channels:history` and `groups:history` scopes, so it reads every channel
  the app can post to. `oldestTimestamp` and `latestTimestamp` bound the range
  by Slack message timestamp, `isInclusive` keeps messages at exactly those
  timestamps, `pageSize` is 1 through 15 (blank or 0 reads 15), and `cursor` continues from
  `nextCursor`. Each message carries its timestamp, poster user and bot IDs,
  text, and, for a thread parent, its thread timestamp and reply count. Thread
  replies appear only when also sent to the channel. Slack limits
  non-Marketplace apps installed since May 2025 to 15 messages per page and one
  history request per minute, so its 429 responses retry after `Retry-After`.
  An application whose `PostChannelMessage` selected `uncertain` can search
  this history for marker text in messages its own bot posted, matching the
  `GetWorkspaceIdentity` bot ID or bot user ID, before posting again; see
  [`examples/channel-marker-search`](go/examples/channel-marker-search).
- `GetThreadReply` reads one reply by timestamp.
- `PostChannelMessage` creates a top-level channel message.
- `PostThreadReply` replies to an existing thread.
- `PostButtonMessage` (Go only) posts a message with buttons; see
  [Buttons](#buttons).

Every query classifies the HTTP status before decoding the body, because Slack
may answer 429 or 5xx without a JSON body. HTTP 429 and 5xx return Retry and
honor `Retry-After` when present; only then does an oversized or malformed body
select `invalidResponse`. Go v1.5.1 and the Python and TypeScript v0.4.1
packages apply this to `ListThreadMessages` and `GetThreadReply`; Go v1.6.0
and TypeScript v0.5.0 also apply it to `ListChannelMessages`.

Posting sends the stable Dex Call ID as Slack `client_msg_id`. [Slack's
`chat.postMessage` reference](https://docs.slack.dev/reference/methods/chat.postMessage/)
does not list `client_msg_id` as an argument; it mentions the field only in
duplicate-related error descriptions. Its replay deduplication behavior remains
unverified. A connection loss after dispatch, a 409, other 5xx, an `ok:false`
`internal_error` or `fatal_error` without a timestamp, or a successful response
without a usable message identity selects `uncertain` and requires reconciliation.
The connector does not resend after such an outcome. A traced DNS, connect, or
TLS failure before a connection exists, HTTP 429, and HTTP 503 return Retry.
An HTTP 2xx carrying
`ok:false` `ratelimited` or `service_unavailable` also retries after recording
that the request was not processed. [Slack's method error descriptions](https://docs.slack.dev/reference/methods/chat.postMessage/)
describe rate limiting with `Retry-After` and temporary service unavailability;
their HTTP statuses and live delivery behavior remain unverified. An
`internal_error` or `fatal_error` with a returned timestamp is read back;
without one it remains `uncertain`. A complete HTTP 2xx `ok:false` post with
`invalid_auth`, `token_expired`, or `token_revoked`, or any HTTP 401, forces a
credential refresh within the Step deadline margin. If refresh supplies a
changed valid bot token, the fence permits one retry on the next Step attempt
with the same `client_msg_id`; a failed, invalid, or unchanged refresh selects
`providerRejected`. The post is never sent twice in one attempt.
Slack's 409 behavior remains unverified.
The shared single-dispatch fence uses synchronous Step durability to persist
its claim and safe-retry evidence.
A known accepted message timestamp is checkpointed for read-back through
`conversations.replies`; an unavailable read-back retries without a new post,
while an inconclusive read-back remains `uncertain`. Read-back uses the remaining
Step context; a timeout retries read-back on the next attempt without posting.
Before a post, a transient credential lookup failure records `notSent` and
retries. Reauthorization-required or missing bot credentials record `notSent`
and select `defect` with an authentication failure and reconnect guidance.
During read-back, a transient credential failure retries without another post;
reauthorization-required or missing user credentials select `uncertain` with
the known message timestamp in the receipt and reconnect guidance.
For replies, whether Slack excludes the parent message when `oldest` and `latest`
name the reply and `limit=1` is unverified. A parent-only result selects
`uncertain`. Missing or stale checkpoints do not authorize a resend. Released
versions never wrote a fence marker: a post Step retry spanning the upgrade
from v1.1.1 at attempt two or later selects `uncertain` without posting.
The fence bounds each request inside the Step attempt. An application must not
override these post Steps to async durability, which cannot preserve the
fence's heartbeat across retries.

Every operation uses `defect` for invalid local input, terminal connection
credential failures, connection configuration, or Connector contract violations.
A conclusive Slack API refusal uses
`providerRejected`; query responses that are malformed or exceed configured
limits use `invalidResponse`. Safe query transport, rate-limit, and availability
failures retry instead of producing a branch.

## Test with slackmock

The generated `slackmock` package answers each operation's happy path by
default. Pass its typed connection to the example Flow to test without Slack
credentials or response scripts:

```go
mock := slackmock.New(t, ConnectionName)
flow, harness := newSlackIntegrationHarnessWithConnection(t, mock.Connection())
harness.startWorker(t)
```

This snippet comes from
[`workflow_mock_integration_test.go`](go/examples/thread-approval/flow/workflow_mock_integration_test.go).
The mock also provides named cases for provider refusals, missing replies,
invalid responses, and uncertain post outcomes.

## Example

[`examples/thread-approval`](go/examples/thread-approval) combines both Triggers,
`ListThreadMessages`, a typed reply RPC, and `PostThreadReply` in one runnable
Flow. [`examples/channel-lookup`](go/examples/channel-lookup) reads the
workspace identity and one filtered channel page.
[`examples/channel-marker-search`](go/examples/channel-marker-search) pages
through recent channel history until it finds the bot's own marker post.
[`examples/button-approval`](go/examples/button-approval) replies to a request
with Approve and Reject buttons and completes on the first decider's click,
with the request and button Triggers on one shared runner.

## Python package

The [Python package](python/README.md) provides all six Slack operations:
`listChannels`, `getWorkspaceIdentity`, `listThreadMessages`, `getThreadReply`,
`postChannelMessage`, and `postThreadReply`. It uses generated OAuth credential decoding and refresh for
private or shared Dex project connections. Socket Mode Triggers remain Go-only.
The runnable [thread messages example](python/examples/thread_messages/README.md)
posts through a Mutation Step and reads the new thread through a Query Step.
Its integration test uses a real Dex Worker and Client with a local Slack HTTP
stand-in; live Slack authorization and Web API behavior are unverified.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the connector owns its real
Worker, Trigger delivery, RPC, persistence, and transition coverage:

```bash
GOWORK=off go test -p 1 -tags=integration ./... -count=1 -v
```
