# Slack thread approval example

This example keeps one complete Slack integration beside the Slack Connector:

1. a matching top-level channel message starts a Flow;
2. the Flow reads the first page of that thread with `ListThreadMessages`;
3. a matching reply invokes the typed `ReceiveThreadReply` RPC; and
4. the Flow posts `Processing complete` to the same thread with `PostThreadReply`.

The root and reply Triggers use the same application callback to map Slack team
ID, channel ID, and root timestamp to a stable Flow ID. The RPC is registered
and invoked through the same direct bound `ReceiveThreadReply` method. The
application RPC locks its thread state and stores the one accepted reply event
ID, so duplicate delivery cannot schedule another completion reply.

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
