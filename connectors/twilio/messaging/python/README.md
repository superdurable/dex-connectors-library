# Twilio Messaging Python connector

`dex-connector-twilio-messaging` provides Dex Steps for `sendMessage` and `getMessage` and the `messageReceived` poll Trigger. The send operation uses Dex's single-dispatch heartbeat fence because Twilio Messages has no idempotency key. An unknown outcome resends while possible sends stay below the send cap, so a recipient may receive a duplicate; at the cap it selects the optional `uncertain` branch, and the application must decide how to reconcile it before another send. A definite refusal is never resent.

Use `new_project_connection(project, "twilio-messaging")` in a Worker and bind `new_send_message_step` or `new_get_message_step` to the static connection name. See [`examples/text_message_delivery`](examples/text_message_delivery) for a runnable Flow and Worker. The connection has Account SID, optional default sender, endpoint, response size limit, poll interval (at least 5 seconds), and either Account SID/Auth Token or API key credentials; setup guidance lives in the [connector manifest](../connector.yaml). Never put credential values or message body in persisted Attributes or logs.

## messageReceived Trigger

`messageReceived` polls the account's Messages list with the Python poll Trigger kit (`dex_connectors.sdk.pollingtrigger`, `dex-connectors-sdk>=0.12.0`) at behavior parity with the Go connector, and delivers one `MessageReceivedEvent` per inbound message: the safe message view plus its text. The binding's saved `to` (an E.164 number or `whatsapp:+E.164` sender; blank receives every inbound message of the account) is sent as the list's `To` filter, and outbound messages are never delivered. The first poll records the last two minutes as a baseline. Later polls relist from the `date_sent` watermark minus two minutes, skip SIDs already delivered, hold an MMS still in `receiving` for up to ten minutes, and deliver at most 500 messages per page, oldest first. A 429 pauses the connection for `Retry-After`; an undecodable cursor, or one for another account or number, records a new baseline. The Trigger event ID is `twilio-message-<SID>`. The [connector README](../README.md#messagereceived-trigger) describes pagination and window halving.

The cursor token and keyed snapshot match the Go connector byte for byte, so Go and Python replicas share a binding's cursor, and a message a Go replica recorded in the inbox replays in Python. [`examples/message_received/routing.py`](examples/message_received/routing.py) records each message in the binding's project Trigger inbox before the watermark commits:

```python
route = ProjectMessageReceivedTriggerRoute(
    MESSAGE_RECEIVED_TRIGGER_BINDING, new_inbound_message_target(dex_client, flow)
)
return new_project_message_trigger_runner(project, connection, [route], logger)
```

`new_message_trigger_runner` takes targets and cursor stores directly, and `new_message_received_trigger` runs one binding with an in-memory cursor, so every start records a new baseline. See [`examples/message_received`](examples/message_received/README.md) for the runnable Worker.

## Test

Run `uv sync --group dev` then `uv run pytest` from this directory. Shared Go/Python replay fixtures are in [`conformance/connectors/twilio/messaging`](../../../../conformance/connectors/twilio/messaging). The Trigger tests replay the Go fetcher tests against a local Messages list fake and compare cursor tokens, snapshot keys, request queries, and event payloads with Go-printed values. Local Dex integration tests use `dexcli dev`; a real Twilio account and sender still require authorization in Dex Web Connections, and live Messages listing has not been verified.
