# Gmail thread reply example

This example keeps one complete inbound Gmail integration beside the Gmail
Connector:

1. a matching received root message starts a Flow;
2. the Flow reads that message with `GetMessage`;
3. a matching received reply invokes the typed `ReceiveEmailReply` RPC; and
4. the Flow replies `Processing complete` in the same Gmail thread with `ReplyToMessage`.

Both neutral provider Triggers use the same application callback to map the
authorized primary email and Gmail thread ID to a stable Flow ID. The
application chooses a Flow-start target for `messageReceived` and an RPC target
for `replyReceived`; those choices are not encoded in the Connector manifest.

## Configure

Follow the [Gmail Connector setup](../../README.md), then configure the
`gmail-inbox` connection and these Trigger bindings in Dex Web:

- `gmail-thread-reply-start` for `messageReceived`;
- `gmail-thread-reply-received` for `replyReceived`.

Both bindings accept an optional Gmail `searchQuery`, case-insensitive
`messageContains`, and sender allowlist. The example uses the Gmail thread ID
as its durable identity.

The alpha local Trigger transport polls the newest inbox messages. It derives
each event ID from the immutable Gmail message ID. A process restart may rescan
the current page, but deterministic Flow starts and the Trigger RPC's durable
event-ID set absorb duplicate delivery. Production push delivery and durable
Gmail history checkpoints are outside this local example.

## Run

Start Dex, then run the example with the connection file shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/thread-reply
```

The default Worker address is `127.0.0.1:8814`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed.

If Gmail rejects the initial read, the Flow fails. A rejected, uncertain, or
defective reply enters `needsRecovery`; the example never blindly resends an
uncertain external write.
