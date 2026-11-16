# Slack thread approval Python example

This example mirrors the Go `thread-approval` example:

1. a matching top-level channel message starts a Flow through the `channelThreadCreated` Trigger;
2. the Flow reads the first page of that thread with `new_list_thread_messages_step`;
3. a matching reply invokes the typed `record_thread_reply` RPC through the `threadReplyCreated` Trigger; and
4. the Flow posts `Processing complete.` to the same thread with `new_post_thread_reply_step` and completes.

Both Triggers build the same Flow ID from the Slack team ID, channel ID, and root timestamp. The Flow start uses the event ID as its request ID, so a redelivered root starts one Flow. The RPC locks the thread state and stores the accepted reply event ID, so a redelivered reply cannot schedule a second completion. A reply whose Flow never started or already completed is consumed as undeliverable and logged. A reply that arrives while the Flow still reads its root raises in the RPC and is retried until the Flow waits for it.

[`receiver.py`](receiver.py) runs both bindings on one `new_project_message_trigger_runner`, because Slack sends each event to only one Socket Mode connection; do not run two copies against the same Slack app. Before either Dex call, each target applies a filter built from the binding configuration saved by Dex Web, checking the channel, root or reply shape, allowed members, and message substring again. The Flow Definition Graph 2.0 source is [`flow.py`](flow.py); each application Step type has one `dex:group` and one `dex:explanation` directive, the state Attribute is registered in the persistence schema, and optional connector branches are unwired, so they fail the Flow if selected.

## Configure and run

1. Create and authorize the Slack app as described in the [connector guide](../../../README.md), subscribe it to `message.channels` and `message.groups`, and invite it to the approval channel.
2. In Dex Web **Connections**, select `slack / slack-workspace` and authorize it. Paste the `xapp-` app-level token in `app_token` to receive events through Socket Mode; on a shared connection whose host relays Slack events, leave it blank.
3. Configure `slack-thread-approval-start`: **Approval channel** (required) picks the joined channel and stores its ID, such as `C0123456789`; **Start message contains** is optional text the root must contain, ignoring case; **Members allowed to start** empty allows any member.
4. Configure `slack-thread-approval-reply`: **Approval channel** picks the same channel; **Approval reply contains** is optional text such as `approved`; **Members allowed to approve** needs at least one member.
5. From `connectors/slack/python`, with `dexcli dev` running, start the application:

```sh
DEX_CONNECTOR_CONFIG_FILE=<path to connections.json> DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run --with uvicorn python -m examples.thread_approval.main
```

In relayed mode the host posts events to `/triggers/slack/c2xhY2std29ya3NwYWNl` on `TRIGGER_BIND_ADDRESS` (default `127.0.0.1:8839`); the runner registers that route with the host. Set `LOG_LEVEL=DEBUG` to see every Slack message the Triggers ignore. Restart the process after changing a binding.

## Test with Dex

`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/thread_approval/integration_test.py` runs this application against a real Dex Worker and Client in both modes, with local stand-ins for Slack's Socket Mode, the Slack Web API, and the host's relay and credential broker. The Socket Mode test acknowledges a duplicate root and reply once each into one completed Flow and consumes an orphan reply; the relayed test refuses a forged callback, then starts and approves the Flow from signed callbacks with empty inboxes afterwards. Live Slack and a live host relay are not verified.
