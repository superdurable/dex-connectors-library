# Gmail Connector

The Gmail Connector receives root messages and replies, reads one message, and
sends new messages or replies from the verified primary address.

The OAuth grant requests exactly these scopes:

- `openid`
- `https://www.googleapis.com/auth/userinfo.email`
- `https://www.googleapis.com/auth/gmail.readonly`
- `https://www.googleapis.com/auth/gmail.send`

The manifest names each Google scope by its canonical URI. Google's token
response reports a granted `email` alias as
`https://www.googleapis.com/auth/userinfo.email`, and Dex Web compares the
granted scopes with the manifest literally, so an alias fails every real grant
with `CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`. Versions v0.11.1 and earlier
requested the `email` alias, so Dex Web from Dex CLI v0.12.0 rejects their
Google grants. After upgrading, a host that stored the requested `email` string
instead of Google's granted scope string reports an existing connection as
needing more permission; reconnect it once.

The connector does not modify or delete existing messages. It supports two
authorization methods:

- `google-oauth` is recommended for personal Gmail and ordinary Workspace
  users. Dex requests offline access, derives the verified primary email from
  Google UserInfo, and refreshes the access token before expiry.
- `workspace-domain-delegation` is an administrator-only option. It signs a
  service-account assertion for the configured managed user, mints a delegated
  access token, and derives the primary email from that delegated user.

The provider-specific refresh driver never owns persistence. The project
connection that `NewProjectConnection` opens admits one refresh per credential
generation across application replicas and stores the complete replacement
before the call uses it; OAuth client secrets, refresh tokens, and
service-account keys stay in encrypted project storage and never enter a Flow.
A refresh token that Google rotates is stored with the new access token; when
Google omits a new one, the prior value is retained. `invalid_grant` requires
reauthorization. After a 401 the connector asks once for a refresh, which the
project connection performs only when the stored expiry has passed, and then
retries once; otherwise the 401 is an authentication failure, never an
uncertain send. There is never a refresh loop.

`messageReceived` and `replyReceived` are neutral provider Triggers. Their
manifest does not decide whether an event starts a Flow or invokes an RPC. The
application passes `NewDexFlowTriggerTarget`, `NewDexRPCTriggerTarget`, or a
custom typed target to the generated Trigger factory. Both Dex targets receive
an application `FlowIDResolver`; the example builds a PII-safe readable ID from
the connection name and Gmail thread ID.

Both Dex targets also require an application-owned `TriggerFilter`. It
runs before Flow ID resolution and is the final admission rule for starting a
Flow or invoking an RPC. Provider search and matcher configuration reduce inbox
traffic, while the application filter can independently enforce root-or-reply,
sender, text, tenant, or other domain rules. Returning false consumes the event
without resolving a Flow ID, mapping input, or calling Dex. Filters, resolvers,
and mappers are deterministic, side-effect-free functions without error results.

The local Trigger transport polls the newest inbox page. `searchQuery` accepts
a Gmail search expression. `MessageMatcher` optionally filters the sender and
a case-insensitive substring across subject and snippet. Gmail message ID is
the stable event ID. Restart rescans can redeliver the current page, so Flow
start request IDs and application-owned RPC state perform final deduplication.
The application chooses the durable key, retention policy, locks, and duplicate
response.

Use one `NewProjectMessageTriggerRunner` for every Gmail message Trigger route
on a connection. It persists each matched event in its binding's project Trigger
inbox before delivering it, and then:

- lists every reply route before any root route in each poll, then delivers
  every root before any reply. A reply's root reaches Gmail first, so the root
  of every reply the runner delivers has already reached Dex;
- consumes an undeliverable event, such as a reply in a thread whose Flow
  finished or never started, and keeps scanning;
- ends the poll at any other failure, such as a Dex outage. Every later poll
  retries that event first, before any later message, until the target
  consumes it, even after newer mail pushes it off the polled page;
- replays pending events at startup, roots first, with backoff from 250
  milliseconds to 30 seconds, and never ends the process because of one event.

The generated per-Trigger factories run independent pollers that do not order
roots before replies. Since v0.11.0, which requires sdkgo v0.9.0, an RPC target
consumes a reply whose Flow does not exist yet. A reply that its poller sees
before the root poller starts the Flow is therefore lost, where v0.10.0 and
earlier retried it. Applications that ran separate root and reply pollers must
switch to the shared runner whenever one Trigger starts a Flow and another
continues it.

The runner logs through `log/slog`, to `slog.Default()` unless you pass
`gmail.WithLogger(logger)` to `NewProjectMessageTriggerRunner` or `New`; the
logger also reaches the durable inboxes that `NewProjectMessageTriggerRunner`
creates. A failed poll or delivery is retried on the next poll, so its `delay`
is the poll interval:

| Level | Message | Emitted by | Attributes |
| --- | --- | --- | --- |
| WARN | `gmail poll failed; retrying` | the poller, when a list, a message read, or the inbox write fails | `attempt`, `delay`, `error`, and `thread_id` and `event_id` when one message failed |
| WARN | `trigger delivery failed; retrying` | the poller, when the target fails | `thread_id`, `event_id`, `attempt`, `delay`, `flow_id` for a Dex error, `error` |
| INFO | `trigger delivered after retry` | the poller, when a later poll delivers that event | `thread_id`, `event_id`, `attempts` |
| WARN | `trigger event skipped: undeliverable` | the durable inbox with `NewProjectMessageTriggerRunner`; the poller without an inbox | `thread_id`, `event_id`, `flow_id` for a Dex error, `error` |
| DEBUG | `trigger event ignored` | the poller | `thread_id`, `event_id`, and `reason`: `not_a_reply`, `not_a_root`, or `matcher_mismatch` |

Every record carries `connector`, `connection`, `trigger`, and `binding`.
`event_id` is the Gmail message ID. The poller passes `thread_id` to the
inbox's and the Dex targets' records too, so one `thread_id` finds every record
about a thread; events that the inbox replays at startup carry only their
`event_id`. `attempt` on a failed poll counts the polls that failed in a row
and starts again after a poll that reaches Gmail. `attempt` on a failed
delivery counts that event's failed deliveries. A failed event that is later
consumed as undeliverable logs only the skip, never `trigger delivered after
retry`. Records never contain senders, recipients, subjects, snippets, bodies,
the search query, or tokens. The runner also emits the durable inbox's replay
and I/O records described in the SDK README.

The generated per-Trigger factories pass `WithLogger` only to their poller,
and their poller records carry no `binding`. Their durable inbox and runner
records go to `slog.Default()`, so call `slog.SetDefault` when you use them,
or use `NewProjectMessageTriggerRunner`.

`GetMessage` returns decoded headers, text, HTML, snippet, labels, and received
time. `ReplyToMessage` reads the source metadata and sends with Gmail thread
ID, `In-Reply-To`, and `References` preserved.

The stable Connector idempotency key is written into the RFC `Message-ID` and
safe correlation header. Gmail documents no deduplication by `Message-ID`, so
the connector does not rely on it and confirms possibly sent requests instead:

- A 2xx with a Gmail message ID is `sent`, with no search. When Gmail omits the
  thread ID, one read of the message supplies it; if that read fails, a reply
  reports the requested thread and a new message an empty thread ID.
- A request that may have reached Gmail but lost its answer, got a 5xx other
  than 503, or got a 2xx without a message ID triggers one Sent Mail search,
  `in:sent rfc822msgid:<Message-ID>` in Gmail's
  [documented search syntax](https://support.google.com/mail/answer/7190).
  Any match is `sent` with Gmail's message and thread IDs, also when a reply
  landed in another thread. No match, or a search that fails, selects
  `uncertain`, which is never resent.
- Only a 429, a 503 whose search finds no message, and a failure that a traced
  DNS, connect, or TLS error proves happened before a connection existed return
  Retry.

A 503 retry sends the message again under the same `Message-ID`. If Gmail
accepted the request despite the 503 and the search could not see it yet, the
recipient can receive two copies. Releases before v0.23.2 reported every
ambiguous outcome, 503 included, as `uncertain` instead of retrying it.

A request whose dispatch cannot be traced counts as possibly sent. That
includes every failure through a `WithHTTPClient` transport that does not drive
`net/http/httptrace`, so such a client turns would-be retries into searches and
`uncertain`.

Every attempt finishes its requests before its Dex deadline, so the engine
never times the attempt out and runs it again. The available time is the Step
context deadline that Dex sets, or else the manifest's 30-second Execute
timeout. The send ends by seven twelfths of it, the search and thread read by
five sixths, and the rest covers the Result write. A reply's source read ends
early enough to leave the send a tenth. A send that runs out of time becomes a
search, never a resend; with less than a tenth left, the attempt returns Retry
before sending. With the default async durability, Dex first runs the Step
under a much shorter deadline, 7 seconds on the Temporal engine, so the send
gets about 4 seconds and the search under 2. A slower send, such as a large
message, is confirmed by that search and selects `uncertain` when Gmail has
not indexed it yet.

The search adds no scope: `gmail.readonly` permits
[`messages.list`](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/list)
with a query. Three Gmail behaviors are unverified without a live account:
whether `messages.send` keeps the client-set `Message-ID` header, whether the
documented query matches it, and how soon a sent message becomes searchable.
If either of the first two fails, the search never matches, so ambiguous sends
select `uncertain`, never a false `sent`. A search miss is not re-checked on a
later attempt. An unwired
`uncertain` branch fails the Flow with its safe failure reason. Wire it only
when a requirement names a reaction, such as asking an operator to check Sent
Mail.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations. A conclusive Gmail API refusal uses
`providerRejected`; message reads that return malformed or oversized data use
`invalidResponse`. Safe query transport, rate-limit, and availability failures
retry instead of producing a branch.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.

Load the project configuration once at application startup and open the
connection by the name its operations and Trigger bindings use, as
[`examples/thread-reply/main.go`](examples/thread-reply/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := gmail.NewProjectConnection(project, threadreply.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
The example passes the same `project` and connection name to
`NewProjectMessageTriggerRunner`, which reads both Trigger bindings' saved
configuration. Set the same `ConnectionName` beside the typed `Connection` in
each operation or Trigger binding: a Step or Trigger whose `ConnectionName` is
empty or differs from its connection's name panics at construction. Dex Web
stores renewable credential material only in encrypted project storage.
Deleting a stored credential does not itself revoke the Google grant.

[`examples/thread-reply`](examples/thread-reply) combines a Flow-start target,
`GetMessage`, a typed reply RPC, and `ReplyToMessage` in one runnable Flow.

## Test with gmailmock

`gmailmock` is the generated mock package for application tests.
`gmailmock.New(t, connectionName)` returns a scripted connection that the
operation Step factories accept, so a Flow runs on a real Worker without the
Gmail API. Its connection serves operation Steps only; Trigger runners need a
connection with a client. An unscripted call answers with its operation's
manifest default: `customerQuestion` for `getMessage`, `sentToCustomer` for
`sendMessage`, and `repliedInThread` for `replyToMessage`. An unexpected send
therefore succeeds silently, so a test of a Flow that sends should assert the
mutation's `Calls()`, or call `Strict()` to require a script for every call. The
thread-reply example's
[mock test](examples/thread-reply/flow/workflow_mock_integration_test.go)
scripts one reply per Flow:

```go
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(sentThreadID))).Respond(gmailmock.ReplyToMessageRepliedInThread())
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(uncertainThreadID))).Respond(gmailmock.ReplyToMessageConnectionLost())
```

The uncertain reply fails the example Flow, which leaves that optional branch
unwired, and `Calls()` shows it was sent once and never resent. Each
operation's manifest mocks cover every branch: for example
`GetMessageDeletedMessage`, `SendMessageInvalidRecipient`, and
`SendMessageConnectionLost`, with `<Operation>Retry` for a retried attempt.
Scripting is described in
[`sdkgo/connectormock`](../../../sdkgo/README.md#connector-mocks).

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the same module owns its real
Worker, Trigger delivery, RPC, persistence, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```
