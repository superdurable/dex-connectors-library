# Twilio Messaging Python connector

`dex-connector-twilio-messaging` provides Dex Steps for `sendMessage` and `getMessage`. The send operation uses Dex's single-dispatch heartbeat fence because Twilio Messages has no idempotency key. A response whose outcome cannot be confirmed selects the optional `uncertain` branch; the application must decide how to reconcile it before another send.

Use `new_project_connection(project, "twilio-messaging")` in a Worker and bind `new_send_message_step` or `new_get_message_step` to the static connection name. See [`examples/text_message_delivery`](examples/text_message_delivery) for a runnable Flow and Worker. The connection has Account SID, optional default sender, endpoint, response size limit, and either Account SID/Auth Token or API key credentials; setup guidance lives in the [connector manifest](../connector.yaml). Never put credential values or message body in persisted Attributes or logs.

Run `uv sync --group dev` then `uv run pytest` from this directory. Shared Go/Python replay fixtures are in [`conformance/connectors/twilio/messaging`](../../../../conformance/connectors/twilio/messaging). A local Dex integration test uses `dexcli dev`; a real Twilio account and sender still require authorization in Dex Web Connections.
