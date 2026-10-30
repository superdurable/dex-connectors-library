# Stripe TypeScript connector

`@superdurable/connector-stripe` provides the same five API operations as the Go Stripe connector: hosted card and ACH Checkout Session creation, Checkout Session lookup, PaymentIntent lookup, and refund creation. The package uses `@superdurable/connectors-sdk` for Dex operation and connection contracts. Its `mock` export supplies typed manifest scenarios for application tests.

## Install

```sh
npm install @superdurable/connector-stripe @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 for the Dex SDK. The connector's `spec.languages.typescript.version` in `connector.yaml` is the published package version; it is independent of the Go connector version.

## Configure a connection

Create a restricted `rk_test_` or `rk_live_` key in [Stripe Dashboard > Developers > API keys](https://dashboard.stripe.com/apikeys). For PaymentIntent reads and refunds, grant PaymentIntent, Charge, and Refund read access plus Refund write access. Choose **API key only** in Dex Web Connections if the application uses only these API operations. Choose **API key and webhook** only when a separate webhook receiver is configured, and store the matching `whsec_` signing secret in that connection. Keep test and live modes aligned.

Load the project's connection at Worker startup with `loadFromEnvironment` from `@superdurable/connectors-sdk/projectconfig`, then call `newProjectStripeConnection(project, ConnectionName)` with a static name defined by the application. Ordinary settings are pinned at startup; the private key is resolved immediately before each provider request. Never put the key in Flow input, Attributes, results, Streams, logs, or mock reports. For isolated tests, pass a `StaticCredentialProvider` holding a `SecretString` to `StripeClient` and bind it with `newStripeConnection`.

Dex Web release identity and FDG 2.0 recognition for TypeScript depend on Dex CLI/Web R4 support. Until that release is available, a TypeScript Flow can run through Dex, but Dex Web Connections may not discover its operation configuration automatically. Configure the connection through the private project configuration path and verify it at Worker startup; do not put credentials in application code or Flow state.

## Operation behavior

`createCardCheckoutSession` and `createACHCheckoutSession` send Stripe's form-encoded `mode=payment` request with `card` or `us_bank_account`, respectively. Both copy bounded nonsecret metadata to the Session and PaymentIntent, and use the stable connector Call ID as `Idempotency-Key`. `createRefund` sends the same key and a `metadata[dex_call_id]` marker. After a nonretryable 5xx, it reads at most three pages of 100 refunds by PaymentIntent to find that marker. A confirmed match returns `refunded`; an unavailable read-back retries; an unconfirmed result returns `uncertain`.

All three writes retry 429, an in-progress 409, and safe 5xx responses using the same key. A 409 with `idempotency_error` or `Stripe-Should-Retry: false` selects `providerRejected`. An unhinted 500, a replayed 5xx, or a server response with `Stripe-Should-Retry: false` is uncertain for Checkout creation, and triggers bounded read-back for refunds. The generated one-hour retry budget remains inside Stripe's key retention window. Any override must keep the complete retry window at most 23 hours.

Queries return `found`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. Writes return their happy path (`created` or `refunded`), `providerRejected`, `uncertain`, `invalidResponse`, or `defect`. Every route except the happy path is optional; an unwired route fails the Flow when selected. Provider response bodies are bounded; oversized successful responses select `invalidResponse`, while oversized error responses use HTTP status and headers for classification. The provider transport pins checked DNS addresses, rejects unsafe destinations, and does not follow redirects.

No live Stripe credentials are included with this package. Tests cover a local HTTP fixture and scripted mock; they do not verify live Stripe authorization, key permissions, Checkout settlement, or refund reconciliation against a real Stripe account.
