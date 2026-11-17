# Typeform response intake

`ResponseIntakeFlow` starts once per signed Typeform `form_response` and stores only its form ID, response token, stable Trigger event ID, and submission time. The receiver checks `Typeform-Signature` against the connection's `webhook_secret`, saves each accepted event in the binding's durable inbox before HTTP 200, and replays pending events after restart. The typed `FormResponseEvent` delivered to the Flow target includes answers, field refs and titles, hidden fields, variables, score, and ending ref; this example intentionally keeps those respondent details out of Flow persistence.

In Dex Web Connections, create `typeform-forms` with a [personal access token](https://admin.typeform.com/user/tokens) that has `forms:read`, `responses:read`, and `webhooks:write`. Create a random secret, copy it into `webhook_secret`, and configure the same secret at [Typeform Webhooks](https://www.typeform.com/developers/webhooks/secure-your-webhooks/) by running the connector's `upsertWebhook` operation for the form. The webhook callback must be a public HTTPS URL ending in `/webhooks/typeform`. In the `response-submitted` binding, save `formId` to the form ID shown in its Typeform URL, such as `https://form.typeform.com/to/u6nXL7`; leave it blank for all forms whose webhooks point to this endpoint. The Flow declares a **Form** picker, but Dex CLI 1.47.9 reports `connector_configuration_ui` for Python SDK objects and emits no picker unit in the FDG. Until that analyzer supports the SDK object, enter `formId` in the binding configuration directly or use the Go example’s picker. Restart the receiver after saving connection or binding changes.

From `connectors/typeform/python`, generate strict FDG 2.0 and start Dex:

```sh
mkdir -p build
uv run dexcli visualize ./examples/response_intake/flow.py --schema-version 2.0 --json --out ./build/response_intake
dexcli dev -flow-rendering-dir ./build
```

Set `DEX_CONNECTOR_CONFIG_FILE` to the connection file saved by Dex Web, then run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run --with uvicorn python -m examples.response_intake.app`. The local listener defaults to `127.0.0.1:8852`; set `WEBHOOK_BIND_ADDRESS` to change it. `/readyz` answers 200 once the binding is active.

For local verification against `dexcli dev`, run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/response_intake/integration_test.py`. It verifies signed delivery, duplicate Flow identity, form filtering, and inbox replay after a process restart. External Typeform authorization and webhook delivery were not exercised locally.
