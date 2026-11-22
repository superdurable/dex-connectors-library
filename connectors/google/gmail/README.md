# Gmail Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/google/gmail/go@v0.29.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/google/gmail`.

## TypeScript (v0.5.1)

Install `@superdurable/connector-google-gmail` with `@superdurable/dex`. The [TypeScript README](typescript/README.md) covers OAuth and Workspace connection setup, the `gmail-inbox` static connection name, the runnable Worker and Client examples, and real Dex tests. The TypeScript package implements `getMessage`, `sendMessage`, and `replyToMessage`, and from 0.3.0 the `messageReceived` and `replyReceived` Triggers at parity with Go: one ordered poller per connection, durable project inboxes, and roots delivered before replies. The [thread reply example](typescript/examples/thread-reply/README.md) mirrors the Go `thread-reply` example. Its manifest-derived npm marker lets Dex CLI render the examples as strict TypeScript FDG 2.0 graphs.

| Capability | Go | Python | TypeScript |
| --- | --- | --- | --- |
| `getMessage`, `sendMessage`, `replyToMessage` | yes | yes | yes |
| `listMessages`, `listThreads`, `getThread`, `listLabels`, `getLabel` | yes | no | no |
| `messageReceived`, `replyReceived` Triggers | yes | yes, from 0.3.0 | yes, from 0.3.0 |

The Gmail Connector receives root messages and replies, reads and searches
messages, threads, and labels, and sends new messages or replies from the
verified primary address.

From Go v0.29.0, the `listMessages` and `listThreads` Queries accept Gmail search syntax,
label IDs, a page token, an optional 1–500 result limit, and an option to
include Spam and Trash. They return IDs and a next token; call `getMessage` or
`getThread` for full content. `getThread` decodes its messages. `listLabels`
returns the mailbox's labels, and `getLabel` returns one label with message and
thread counts. All five Queries are safe to repeat and use the existing
`gmail.readonly` scope. The [search Flow](go/examples/thread-reply/flow/search_mailbox.go)
shows `listMessages` with the existing `gmail-inbox` connection.

These additions do not add Dex Web operation configuration fields. The
example accepts the search query and page limit as Flow input.

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
retries once; otherwise the 401 selects `providerRejected`. There is never
a refresh loop.

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
ID, `In-Reply-To`, and `References` preserved. Gmail pads a body's base64url
data whenever its length is not a multiple of three. Go v0.29.1 message,
thread, and Trigger reads and TypeScript v0.5.1 message reads accept that
padding as well as unpadded data; earlier releases selected `invalidResponse`
for such messages, which live tests found to be most of them, and earlier Go
Triggers could not decode them. Python always accepted it.

The connector derives an RFC `Message-ID` from the stable call key, Dex's
first run ID, and the Flow start second. From Go v0.29.1, Python v0.5.1, and
TypeScript v0.5.1 it repeats that value in an `X-Dex-Message-ID` header; the
safe correlation header `X-Dex-Call-ID` carries the Call ID. Gmail documents no
deduplication by `Message-ID`, and a live account showed that `messages.send`
replaces the client `Message-ID` on the Sent copy with Gmail's own
`<…@mail.gmail.com>` value, also for an ID in the sender's own domain, and
keeps no copy of it, while it keeps `X-Dex-*` headers verbatim. Gmail search
cannot match a custom header, so Sent Mail read-back lists up to 25 recent
candidates with `in:sent newer_than:2d`, adding `to:` and the first recipient
when the recipient is known and its address uses only letters, digits, and
`.`, `_`, `+`, or `-`. A reply's read-back before its source read knows no
recipient. The connector then reads each candidate's `Message-ID` and
`X-Dex-Message-ID` with `format=metadata`. Only a candidate whose
`X-Dex-Message-ID`, or whose `Message-ID` where a compatible gateway kept it,
equals this call's value is a match; a Sent copy of another call, or of an
earlier run of a reused Flow ID with the same `X-Dex-Call-ID`, is not. Within
one poll, a candidate found to be another message is not read again, and a
candidate whose read fails leaves the lookup inconclusive unless another
candidate matches. Before these releases the read-back searched
`rfc822msgid:`, which never matches the rewritten `Message-ID`, so a crash or
lost answer after Gmail accepted a send was resent up to the send cap:

- A 2xx with a Gmail message ID is `sent`, with no search. A body names a
  message ID when it is a JSON object whose `id` is a non-empty string, whatever
  its other fields are, in Go, Python, and TypeScript alike. When Gmail omits
  the thread ID or sends one that is not a string, one read of the message
  supplies it; if that read fails, a reply reports the requested thread and a
  new message an empty thread ID.
- A 2xx whose body cannot be read, is oversized or invalid, or lacks an ID is
  already accepted, including a body that breaks or stalls after the headers
  arrived and a write deadline that ends after them. The connector polls Sent
  Mail several times. A match
  supplies the IDs; otherwise `sent` has an empty `messageId` and the requested
  reply thread ID, or an empty thread ID for a new message. It never resends
  that accepted request.
- A request that may have reached Gmail but lost its answer or got a 5xx polls
  Sent Mail several times with that candidate read-back, in Gmail's
  [documented search syntax](https://support.google.com/mail/answer/7190).
  The lookup requests Spam and Trash inclusion.
  Any match is `sent` with Gmail's message and thread IDs, also when a reply
  landed in another thread. A miss or failed lookup returns Retry.
- Every numbered attempt after the first tries read-back before reading a
  reply's source or posting. A match returns `sent` without another POST; for a
  reply its `recipients` can be empty because the source was never read. After
  a `notSent` or `canResend` checkpoint, a miss permits another POST and a
  failed search returns Retry.
- `sendMessage` and `replyToMessage` declare `repeatOnUnknown: limited` with
  `maxSends: 3`. After a prior `claimed` dispatch whose outcome stayed unknown,
  a Sent Mail miss or failed search resends with the same `Message-ID` while
  fewer than `call.MaxSends()` sends may have taken effect, counting every
  earlier attempt that may have sent. From Go v0.28.0, Python v0.5.0, and
  TypeScript v0.5.0, both operations declare an optional `uncertain` branch:
  when the final send the cap allows stays unknown and its Sent Mail poll finds
  no match, or a later attempt at the cap, or with a saved message ID, cannot
  confirm the send, the attempt selects `uncertain` without another POST.
  Earlier versions returned Retry until the retry budget failed the Flow. A
  confirmed read-back still selects `sent`. A POST status of 400, 401, 403, 404,
  411, 413, 414, 415, or 422 proves Gmail refused the POST before processing
  it, unless its body names a Gmail message. With no earlier possible send, it
  records `canResend` and selects `providerRejected`, and is never resent. Any
  other 4xx except 429, such as 408 or 409, an allowlisted status whose body is
  unreadable, or an unreadable or untrusted checkpoint,
  leaves the outcome unknown: below the cap it returns Retry and keeps the
  claim, so the next attempt reads back Sent Mail and resends; on the final
  send the cap allows it selects `uncertain`. From Go
  v0.28.0, Python v0.5.0, and TypeScript v0.5.0, a non-2xx body whose `id` is a
  non-empty string means Gmail may have sent the message: the ID is
  checkpointed and read back with `format=minimal`, a match selects `sent`, and
  otherwise the attempt returns Retry; later attempts read that ID back and
  search Sent Mail but never resend it, and from Go v0.28.0 select `uncertain`
  when neither confirms it. Every attempt with a checkpointed ID
  reads it back by ID before searching Sent Mail. Before any possible send, a
  transient credential failure records `notSent` and returns Retry; only
  reauthorization, invalid credential material, a missing project object, an
  unselected connection, or a broker rejection selects `defect`. After an
  earlier possible send, an allowlisted 4xx on the resend, or an allowlisted 4xx
  on the reply source read, selects `uncertain` instead, because it proves only
  that this attempt failed while the earlier send may still be delivered. An
  unusable reply source read Retries while the cap permits another send, so
  later attempts read back, and selects `uncertain` when this attempt's send
  would be the last the cap allows. After an earlier possible send, `defect` is
  never selected: invalid input selects `uncertain`, a transient credential failure
  returns Retry without a `notSent` checkpoint, and a credential failure that
  needs reconnection selects `uncertain`, because no later attempt could read
  back or resend. The same holds for a
  failed pre-send search or reply source read after a `canResend` checkpoint
  that still counts an earlier send: the attempt returns Retry and records
  nothing, so the next attempt still reads back first. Gmail does not deduplicate by
  `Message-ID`, so an unknown outcome can deliver up to three copies when Sent
  Mail indexing lags an accepted send. An application changes one Step's cap
  with the factory's `MaxSends` field, from 1 (never resend) to 10.
- Dex agent hosts honor the cap from Dex Agents 0.6.12. Enable these tools with
  a cap above 1 only after no Worker older than 0.6.12 runs that session Flow
  type, and do not roll back below 0.6.12 while a bounded call is open. A cap of
  1 is safe on any Worker.

The Go send result marshals an empty recipient list as JSON `[]` on every
terminal branch, including an early read-back and a provider rejection.
- When a prior checkpoint contains Gmail's message ID, the connector reads
  that message directly before searching Sent Mail. A matching read
  returns `sent`; a failed read falls back to the Sent Mail search. A saved
  message ID proves Gmail accepted the send, so it is never resent.
- A 429 and a failure that a traced DNS, connect, or TLS error proves happened
  before a connection existed return Retry.
- A 503 polls Sent Mail like any other 5xx. A match is `sent`; a miss returns
  Retry and keeps the `claimed` checkpoint, so the next attempt reads back and
  resends only below the send cap, or selects `uncertain` at the cap.

A send or reply records a `claimed` heartbeat, with its `sendCount`, before the
POST. Synchronous Execute durability carries that checkpoint across regular
retries. A 429 or proven undispatched transport failure can checkpoint
`canResend`, allowing one later POST after a Sent Mail search miss. Gmail's
[error guide](https://developers.google.com/workspace/gmail/api/guides/handle-errors)
describes 5xx responses, 503 included, as errors raised while processing the
request, and Sent Mail search can lag an accepted send, so a 503 counts as a
possible send. The manifest retry policy allows seven attempts within five
minutes: three sends, the Sent Mail search before each resend, and their
rate-limit and credential retries. At the send cap, a Sent Mail search that
finds no match, or that fails transiently, selects `uncertain` instead of
Retry, because a failed Flow invites a person or agent to send again.
Gmail does not
dedupe by `Message-ID`; a lost heartbeat frame also cannot prove exactly-once
delivery.
The released v0.23.1 selected `uncertain` for an ambiguous send and did not
search Sent Mail; from v0.28.0 `uncertain` is selected only after Sent Mail
read-back at the send cap.
The run-specific Message-ID reduces false matches when a Flow ID is reused or
a Flow retries, although two runs with the same first run ID and Flow start
second can still collide.

A request whose dispatch cannot be traced counts as possibly sent. That
includes every failure through a `WithHTTPClient` transport that does not drive
`net/http/httptrace`, so such a client searches after a failed POST, and later
attempts resend after a miss only below the send cap.

Each synchronous Execute attempt budgets at most 30 seconds, shortened by an
earlier Dex Step deadline. The fenced POST ends by seven twelfths of that
budget, and read-back and thread lookup end by five sixths; the remaining time
covers the Result write. With the full 30 seconds, those boundaries are 17.5
and 25 seconds. A reply's source read reserves at least a tenth of the budget
for dispatch. If too little time remains, the attempt returns Retry before
posting; a `notSent` checkpoint, when one is written, keeps the count of
earlier possible sends. A dispatched request that times
out searches Sent Mail; a later attempt can POST after its own search miss or
failed search, following a `notSent` or `canResend` checkpoint, or a `claimed`
one while below the send cap.

The read-back adds no scope: `gmail.readonly` permits
[`messages.list`](https://developers.google.com/workspace/gmail/api/reference/rest/v1/users.messages/list)
with a query and `messages.get` with `format=metadata`. A Google Workspace
account verified the `Message-ID` replacement, the kept `X-Dex-*` headers,
that `rfc822msgid:` matches only Gmail's own `Message-ID`, that header values
are not searchable, and that search listed a new Sent copy within a second of
the send's answer. A send whose Worker stopped after Gmail accepted it, retried
under the same Call, found its Sent copy and sent nothing more. The read-back
still misses, and the send is resent up to the cap, when the Sent copy is not
among the 25 newest candidates by the retry, is older than two days, was
deleted, or is not yet searchable. A call whose earlier attempt ran Go v0.29.0,
Python v0.5.0, TypeScript v0.5.0, or earlier posted no `X-Dex-Message-ID`, so
it cannot be confirmed after an upgrade. Personal `@gmail.com` accounts,
Workspace delegation, Trigger polling, and token refresh remain unverified
live.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations; a send or reply uses it only when no earlier
send is possible. A conclusive Gmail API refusal uses
`providerRejected`; message reads that return malformed or oversized data use
`invalidResponse`. Safe query transport, rate-limit, and availability failures
retry instead of producing a branch. A send or reply that Sent Mail read-back
cannot confirm at the send cap uses `uncertain`; applications wire it only to
show a required business reaction, such as asking a person to check Sent Mail.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.

Load the project configuration once at application startup and open the
connection by the name its operations and Trigger bindings use, as
[`examples/thread-reply/main.go`](go/examples/thread-reply/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := gmail.NewProjectConnection(project, threadreply.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
The example passes the same `project` and connection name to
`NewProjectMessageTriggerRunner`, which reads both Trigger bindings' saved
configuration. Set the same `ConnectionName` beside the typed `Connection` in
each operation or Trigger binding: a Step or Trigger whose `ConnectionName` is
empty or differs from its connection's name panics at construction. Dex Web
stores renewable credential material only in encrypted project storage.
Deleting a stored credential does not itself revoke the Google grant.

[`examples/thread-reply`](go/examples/thread-reply) combines a Flow-start target,
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
[mock test](go/examples/thread-reply/flow/workflow_mock_integration_test.go)
scripts one reply per Flow:

```go
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(sentThreadID))).Respond(gmailmock.ReplyToMessageRepliedInThread())
	mock.ReplyToMessage().ForFlow(ResolveFlowID(threadEvent(retriedThreadID))).Respond(gmailmock.ReplyToMessageConnectionLost(), gmailmock.ReplyToMessageRepliedInThread())
```

The retried reply completes, and `Calls()` shows two attempts. Each
operation's manifest mocks cover every branch: for example
`GetMessageDeletedMessage`, `SendMessageInvalidRecipient`, and
`SendMessageConnectionLost`, with `<Operation>Retry` for a retried attempt.
Scripting is described in
[`sdkgo/connectormock`](../../../sdkgo/README.md#connector-mocks).

## Python

The independently versioned Python package is `dex-connector-google-gmail`
(`dex_connectors.google_gmail`). Its [README](python/README.md) covers
installation, the static `gmail-inbox` Dex Web connection, OAuth refresh,
and the [runnable read-and-reply Flow](python/examples/thread_reply/main.py).
The Python operation factories expose the same `getMessage`, `sendMessage`,
and `replyToMessage` branches and retry policy as this connector manifest.
Dex CLI/Web 1.28.0 supports Python FDG 2.0 analysis and Dex Web connection
discovery through this package's generated `dex-connector.json`. The example's
Worker and real Dex integration test cover the connector operations.

From 0.3.0 the Python package also implements the `messageReceived` and
`replyReceived` Triggers at parity with Go: one ordered poller per connection
(`new_project_message_trigger_runner`) that lists replies before roots and
delivers every root before any reply, durable project inboxes that Go and
Python replicas share, and a retry of a failed event that survives newer mail
pushing it off the page. The
[Trigger thread reply example](python/examples/trigger_thread_reply/README.md)
mirrors the Go `thread-reply` example.

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
