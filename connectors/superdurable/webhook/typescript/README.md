# Webhook TypeScript connector

`@superdurable/connector-superdurable-webhook` implements the `sendEvent` Mutation. The incoming `requestReceived` Trigger is available in Go only. The TypeScript operation sends a JSON payload to the connection's HTTPS `deliveryUrl` with Standard Webhooks `webhook-id`, `webhook-timestamp`, and `webhook-signature` headers. The `webhook-id` is stable for a Dex Call; receivers must deduplicate it for at least the effective retry window.

Use the runnable [send-event example](examples/send-event/README.md) to register a Worker and start a Flow. Set `webhook-events` in Dex Web with a delivery URL and signing secret. The secret stays in the credential broker and does not enter Flow input or output. A 2xx answer selects `delivered`; 408, 409, 425, 429, 5xx, and transport failures retry; other non-2xx answers select `rejected`. Invalid settings, credentials, or payloads select `defect`.

Run `npm run check` for package tests and `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` pointing at `dexcli dev` for the real Dex integration test. Live receiver behavior has not been verified.
