# Gmail thread reply example

This TypeScript example mirrors the Go [`thread-reply`](../../../go/examples/thread-reply/README.md) example:

1. a matching received root message starts a Flow through the `messageReceived` Trigger;
2. the Flow reads that message with `getMessage`;
3. a matching received reply invokes the typed `receiveEmailReply` RPC through the `replyReceived` Trigger; and
4. the Flow replies in the same Gmail thread with `replyToMessage`, using the completion reply saved in Dex Web.

Both Triggers build the same Flow ID from the connection name and the Gmail thread ID. The Flow start uses the event ID as its request ID, so a redelivered root starts one Flow. The RPC locks the thread state and stores the accepted reply event ID, and the RPC target sends a stable request ID, so a redelivered reply cannot schedule a second reply. A reply whose Flow never started is consumed as undeliverable and logged. A reply that arrives while the Flow still reads its root is retried on the next poll.

`app.ts` runs both bindings, `gmail-thread-reply-start` and `gmail-thread-reply-received`, on one `newProjectMessageTriggerRunner` for the `gmail-inbox` connection. Each poll lists replies before roots and then delivers every root before any reply. Before either Dex call, each target applies a filter built from the binding configuration saved by Dex Web, checking the root or reply shape, the sender allowlist, and the subject or snippet substring again.

## Configure

Follow the [package setup](../../README.md#configure-a-connection), then configure the `gmail-inbox` connection and both Trigger bindings in Dex Web. Both bindings accept an optional Gmail search query, a case-insensitive `messageContains`, and a sender allowlist. Also enter the required **Completion reply** for the `ReplyToReceivedEmail` Step; like the Go example, `app.ts` loads it with `loadReplyMessageConfiguration` and refuses to start without it.

## Run

Verify the Flow with Dex CLI from a consumer project that has the package and `typescript` installed:

```sh
dexcli visualize ./examples/thread-reply/flow/workflow.ts --schema-version 2.0 --json --out ./build/gmail-thread-reply
```

Then run it with a Dex project configured by Dex Web inside `dexcli dev`:

```sh
npm run build
DEX_CONNECTOR_CONFIG_FILE=<path to connections.json> DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run example:thread-reply
```

Set `LOG_LEVEL=debug` to see every message the Triggers ignore and every delivery. The [integration test](integration.test.ts) runs this application against a real Dex server with a local stand-in for the Gmail API: it covers ignored messages, an orphan reply, the saved completion reply, a reply retried while its Flow still reads the root, and a restart that rescans the page. Live Gmail is not verified.
