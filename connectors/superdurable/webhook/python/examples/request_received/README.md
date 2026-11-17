# Receive verified form submissions

This runnable Python application accepts signed `POST` requests at
`/webhook/form-submissions`, stores each accepted request in the
`submissions` Trigger inbox, then starts one `WebhookFormSubmissionPython`
Flow per stable event ID. `/readyz` returns 200 after inbox replay and binding
startup. The Flow records the request in a Dex Attribute and completes.

In Dex Web **Connections**, create `webhook-form` with the Webhook connector.
In the sender's webhook settings, enter the public HTTPS URL that forwards to
`/webhook/form-submissions`. Follow the [sender setup](../../../README.md#sender-setup)
for its signing secret and verification scheme. The example accepts JSON bodies
whose `/type` is `form.submitted`; configure the binding `submissions` with
`matchPointer: /type` and `matchValues: [form.submitted]`. Set `eventIdHeader`
to the sender's delivery ID header, or `eventIdPointer` to a unique event ID
field. Blank uses the body SHA-256, which deduplicates byte-identical retries.
The example uses `webhook-form` as a static `ConnectionName`; it requires no
operation-specific UI fields. `signing_secret` stays in Dex Credentials.

Run a Dex development server, set `DEX_FLOW_SERVICE_ADDRESS` and the project
connection configuration as described in the [Python SDK project guide](../../../../../../sdkpython/README.md),
then run from `connectors/superdurable/webhook/python`:

```bash
uv sync --locked
uv run --with uvicorn python -m examples.request_received.main
```

`WEBHOOK_BIND_ADDRESS` defaults to `127.0.0.1:8867`. Publish the endpoint
through an HTTPS tunnel for a real sender. A 503 means the receiver is not
ready or cannot durably store the event, so the sender should retry.
The integration test uses inert credentials and a real Dex Worker; it verifies
acknowledgment, restart replay, deduplication, and sensitive-header removal.
