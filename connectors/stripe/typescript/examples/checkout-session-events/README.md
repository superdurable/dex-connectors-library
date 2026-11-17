# Stripe Checkout Session events

This TypeScript example mirrors the Go Checkout Session webhook receiver and records settled delayed payments in a real Dex Flow. The `registration-payments` binding accepts the four Checkout Session event types; `acceptSettledPayment` admits only `checkout.session.async_payment_succeeded`. The Flow ID is `stripe-<event id>`, so redelivery maps to the same Flow. The Flow stores only the connector-safe Checkout Session snapshot.

In [Stripe Dashboard > Developers > API keys](https://dashboard.stripe.com/apikeys), create a restricted `rk_test_` or `rk_live_` key with the permissions needed by your API operations. In [Developers > Webhooks](https://dashboard.stripe.com/webhooks), create an endpoint at your public HTTPS `/webhooks/stripe` URL, subscribe it to `checkout.session.async_payment_succeeded`, and reveal its `whsec_` signing secret. Save both values in the Dex Web `stripe-payments` connection using **API key and webhook**. Keep test and live modes aligned; both values are secret and remain outside Flow state. Leave optional connection settings blank for the manifest defaults.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` and the private `DEX_PROJECT_*` environment from Dex Web, and run from `connectors/stripe/typescript`:

```sh
npm run example:checkout-events
```

The webhook binds to `STRIPE_WEBHOOK_BIND_ADDRESS` (default `127.0.0.1:8854`), and `GET /readyz` returns `200` after the binding starts receiving. The runner writes each accepted delivery to the project inbox before answering `200`; when Dex is unavailable it retries Worker startup, and after a restart it replays the inbox. Forged, expired, or future signed requests answer `400`; a missing signing secret or inactive binding answers `503`.

Generate strict Flow Definition Graph 2.0 from the package directory:

```sh
npm run build
npm install --prefix examples/checkout-session-events --no-save --no-package-lock --ignore-scripts .
dexcli visualize examples/checkout-session-events/flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` verifies signed delivery, forgery rejection, redelivery deduplication, and restart recovery with a real Dex Worker and Client. Live Stripe webhook delivery and authorization remain unverified.
