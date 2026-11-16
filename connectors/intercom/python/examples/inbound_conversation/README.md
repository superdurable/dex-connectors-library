# Intercom inbound-conversation example for Python

This example starts one `IntercomInboundConversationPython` Flow for every new
Intercom conversation, through the `conversationEvent` Trigger's
`inbound-conversation` binding:

1. `RecordInboundConversation` validates and records the notification's
   identities. No message text enters Flow state.
2. `ReadInboundConversation` calls `new_get_conversation_step` and reads the
   conversation with its five latest parts as plain text.
3. `CompleteInboundConversation` completes with the conversation state, the
   customer's email, and the part count.

`accept_inbound_conversation` admits only `conversation.user.created`, whatever
topics the stored binding selects. The Flow ID is
`intercom-inbound-conversation-<conversation ID>`, and the Trigger event ID is
Intercom's notification ID, so a redelivered notification starts no second
Flow. In Dex Web, Start Flow also accepts `{"conversationId": "<digits>"}`.

`receiver.py` wires the binding to the Flow with
`new_project_conversation_event_endpoint_runner`, which records each signed
notification in the binding's Trigger inbox before Intercom receives `200`, so a
notification that arrives while Dex is unreachable starts its Flow later.
`main.py` serves the receiver with uvicorn at `/webhooks/intercom` beside a
`/readyz` check, and runs the Worker on the same event loop.

## Configure

Follow the [Intercom setup](../../../README.md#intercom-setup), then save the
`intercom-support-inbox` connection under **Connections** in Dex Web with the
app's **Access Token** and **client secret**. A blank client secret makes the
webhook answer `503`.

- On the Intercom app's **Configure > Webhooks** page, enter the public HTTPS
  URL that forwards to `/webhooks/intercom`, such as a tunnel to
  `127.0.0.1:8852`, and subscribe `conversation.user.created`.
- The binding's **Topics that start the Flow** picker stores its topics;
  selecting none accepts every supported topic, and the Flow still starts only
  for new conversations.

The process reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/intercom/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/inbound_conversation/flow.py --schema-version 2.0 --json --out ./build/inbound_conversation
dexcli dev -flow-rendering-dir ./build
```

Open Dex Web at `http://127.0.0.1:8802`, configure the connection, set
`DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote, and
run the receiver and Worker:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run --with uvicorn python -m examples.inbound_conversation.main
```

Set `INTERCOM_WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8852`),
`DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL` when needed. Trigger records, without
payloads or credentials, go to the `dex_connectors.sdk.trigger` logger.

## Test

```bash
uv run pytest tests/test_inbound_conversation_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/inbound_conversation/integration_test.py
```

The integration test runs a real Dex Worker and Client against a fake of
Intercom's Retrieve a conversation API. It acknowledges a signed notification
while the binding's target cannot reach Dex, restarts the receiver, and checks
that the pending notification starts its Flow; that a live notification starts
one Flow that reads the conversation back; and that a redelivery, a customer
reply, and a forged signature start none. Live Intercom webhooks have not been
verified.
