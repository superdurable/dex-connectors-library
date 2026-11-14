# Slack thread approval example

This TypeScript example mirrors the Go `thread-approval` example:

1. a matching top-level channel message starts a Flow through the `channelThreadCreated` Trigger;
2. the Flow reads the first page of that thread with `listThreadMessages`;
3. a matching reply invokes the typed `receiveThreadReply` RPC through the `threadReplyCreated` Trigger; and
4. the Flow posts `Processing complete.` to the same thread with `postThreadReply`.

Both Triggers build the same Flow ID from the Slack team ID, channel ID, and root timestamp. The Flow start uses the event ID as its request ID, so a redelivered root starts one Flow. The RPC locks the thread state and stores the accepted reply event ID, so a redelivered reply cannot schedule a second completion. A reply whose Flow never started or already completed is consumed as undeliverable and logged. A reply that arrives while the Flow still reads its root is retried until the Flow waits for it.

`app.ts` runs both bindings on one `newProjectMessageTriggerRunner`, because Slack sends each event to only one Socket Mode connection; do not run two copies against the same Slack app. When the connection's credential has an `app_token`, the runner uses Socket Mode. When it has none and the connection is shared by a host that relays Slack events, the runner serves them at `relayedTriggerPath('slack-workspace')` on `TRIGGER_BIND_ADDRESS` (default `127.0.0.1:8839`) and registers that route with the host.

Before either Dex call, each target applies a filter built from the binding configuration saved by Dex Web, checking the channel, root or reply shape, allowed members, and message substring again.

Run it with a Dex project configured by Dex Web inside `dexcli dev`:

```sh
npm run build
DEX_CONNECTOR_CONFIG_FILE=<path to connections.json> DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx tsx examples/thread-approval/main.ts
```

Set `LOG_LEVEL=debug` to see every Slack message the Triggers ignore. The [integration test](../../test/integration/thread-approval-triggers.integration.test.ts) runs this application against a real Dex Server in both modes, with local stand-ins for Slack's Socket Mode, the Slack Web API, and the host's relay and credential broker. Live Slack and a live host relay are not verified.
