# Webhook request intake

`WebhookRequestIntakeFlow` starts once for each verified JSON `form_response` request. The endpoint verifies an HMAC-SHA256 signature, saves the event in the binding's durable project inbox before answering HTTP 200, and replays pending events when Dex returns. The sender's stable `event_id` supplies the Flow ID. The Flow persists only the ID, content type, and receipt time; it never persists the request body, forwarded headers, or signing secret.

From `connectors/superdurable/webhook/typescript`, run `npm ci`, start `dexcli dev`, configure the project's `DEX_PROJECT_*` environment, then run `npm run example:request-intake`. The listener defaults to `127.0.0.1:8838`. `WEBHOOK_RECEIVER_HOST`, `WEBHOOK_RECEIVER_PORT`, and `DEX_FLOW_SERVICE_ADDRESS` override it. Mount `/webhooks/receiver` through a public HTTPS URL. `/readyz` answers 200 while the binding is active.

In Dex Web Connections, create `webhook-receiver`. At the sender's webhook settings, enter the public HTTPS URL and create a shared signing secret. Copy it into `signing_secret` in Dex Web; it is secret. For this example, choose `hmacSha256`, the sender's signature header, SHA-256 encoding and any literal prefix, and set `eventIdPointer` to `/event_id`. The sender must sign the raw body and reuse its event ID on redelivery. Set the `submissions` binding's `matchPointer` to `/event_type` and `matchValues` to `form_response`; blank filter settings accept all verified requests, though the example's admission rule still accepts only `form_response`. See the [connector sender setup](../../../README.md#sender-setup) for GitHub, Shopify, Typeform, and Standard Webhooks paths and signature formats.

Generate strict FDG 2.0 after installing this package in an independent consumer directory:

```sh
npm run build
npm install --prefix examples/request-intake --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/request-intake/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/dex-webhook-ts-trigger-fdg --open=false
```

Run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` against `dexcli dev` for signed delivery, redelivery, filtering, and inbox replay after restart. A real external sender was not used in this local test.
