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

`GetMessage` returns decoded headers, text, HTML, snippet, labels, and received
time. `ReplyToMessage` reads the source metadata and sends with Gmail thread
ID, `In-Reply-To`, and `References` preserved.

The stable Connector idempotency key is written into the RFC `Message-ID` and
safe correlation header. Gmail does not promise server-side deduplication, so
a timeout, connection loss, ambiguous 5xx, or invalid success response returns
the `uncertain` branch and is never automatically resent. Applications must
route that branch to explicit operator recovery.

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
