# Gmail Connector

The Gmail Connector receives root messages and replies, reads one message, and
sends new messages or replies from the verified primary address.

The OAuth grant requests `openid`, `email`, `gmail.readonly`, and `gmail.send`.
It does not modify or delete existing messages. The generated
`Credentials` contains a short-lived access token and the verified primary
email; refresh tokens remain in the hosting application's OAuth broker.

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

Use one `NewLocalMessageTriggerRunner` for every Gmail message Trigger route on
a connection. It persists each matched event in a binding-specific inbox before
delivering it, and then:

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
`gmail.WithLogger(logger)` to `NewLocalMessageTriggerRunner` or `New`; the
logger also reaches the durable inboxes that `NewLocalMessageTriggerRunner`
creates. A failed poll or delivery is retried on the next poll, so its `delay`
is the poll interval:

| Level | Message | Emitted by | Attributes |
| --- | --- | --- | --- |
| WARN | `gmail poll failed; retrying` | the poller, when a list, a message read, or the inbox write fails | `attempt`, `delay`, `error`, and `thread_id` and `event_id` when one message failed |
| WARN | `trigger delivery failed; retrying` | the poller, when the target fails | `thread_id`, `event_id`, `attempt`, `delay`, `flow_id` for a Dex error, `error` |
| INFO | `trigger delivered after retry` | the poller, when a later poll delivers that event | `thread_id`, `event_id`, `attempts` |
| WARN | `trigger event skipped: undeliverable` | the durable inbox with `NewLocalMessageTriggerRunner`; the poller without an inbox | `thread_id`, `event_id`, `flow_id` for a Dex error, `error` |
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
or use `NewLocalMessageTriggerRunner`.

`GetMessage` returns decoded headers, text, HTML, snippet, labels, and received
time. `ReplyToMessage` reads the source metadata and sends with Gmail thread
ID, `In-Reply-To`, and `References` preserved.

The stable Connector idempotency key is written into the RFC `Message-ID` and
safe correlation header. Gmail does not promise server-side deduplication, so
a timeout, connection loss, ambiguous 5xx, or invalid success response returns
the `uncertain` branch and is never automatically resent. Applications must
route that branch to explicit operator recovery.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations. A conclusive Gmail API refusal uses
`providerRejected`; message reads that return malformed or oversized data use
`invalidResponse`. Safe query transport, rate-limit, and availability failures
retry instead of producing a branch.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.

For local Dex Web setup, name the factory connection and load the same name at
application startup:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
sender, err := gmail.NewLocalConnection(store, "sender")
```

Set the same `ConnectionName` beside the typed `Connection` in each operation
or Trigger binding. Dex Web stores only the short-lived access token and
confirmed primary email. It does not store a refresh token, and deleting the
local credential does not revoke the Google grant.

[`examples/thread-reply`](examples/thread-reply) combines a Flow-start target,
`GetMessage`, a typed reply RPC, and `ReplyToMessage` in one runnable Flow.
