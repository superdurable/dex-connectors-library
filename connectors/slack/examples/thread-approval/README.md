# Slack thread approval example

This example keeps one complete Slack integration beside the Slack Connector:

1. a matching top-level channel message starts a Flow;
2. the Flow reads the first page of that thread with `ListThreadMessages`;
3. a matching reply invokes the typed `ReceiveThreadReply` RPC; and
4. the Flow posts `Processing complete.` to the same thread with `PostThreadReply`.

The root and reply Triggers use the same application resolver to build a
readable Flow ID from Slack team ID, channel ID, and root timestamp. The RPC is
registered and invoked through the same direct bound `ReceiveThreadReply`
method. An RPC input mapper passes only the event ID and approver user ID. The
application RPC locks its thread state and stores the one accepted reply event
ID, so duplicate delivery cannot schedule another completion reply.

The completion Mutation configures the optional `ResultAttribute`. The Flow
registers that typed Attribute explicitly, and its summary/display RPCs read the
full provider result outside the transition chain for operator inspection. This
intentionally duplicates the durable target input; applications should omit the
Attribute when no external reader needs the raw result.

Before either Dex call, the application supplies a typed filter. The example
builds those filters from the binding configuration saved by Dex Web and checks
the channel, root-or-reply shape, allowed member, and case-insensitive message
substring again. A rejected event is consumed without resolving a Flow ID or
calling Dex. The pure filter has no error result.

## Configure

Follow the [Slack Connector setup](../../README.md), then configure the
`slack-workspace` connection and these Trigger bindings in Dex Web:

- `slack-thread-approval-start` for `channelThreadCreated`;
- `slack-thread-approval-reply` for `threadReplyCreated`.

Choose one channel for both bindings. The root text and poster filters are
optional. The reply binding requires at least one allowed member. Its text
filter can be `approve`, but matching uses case-insensitive substring semantics,
so `disapprove` also matches.

## Run

Start Dex, then run the example with the connection file shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/thread-approval
```

The default Worker address is `127.0.0.1:8813`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed.

If Slack rejects the read, the Flow fails. A rejected, uncertain, or defective
completion reply enters `needsRecovery`; the example never blindly resends an
uncertain external write.
