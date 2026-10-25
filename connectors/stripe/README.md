# Stripe Connector

This module creates and reconciles Stripe-hosted Checkout Sessions that accept
card or US bank account (ACH Direct Debit) payments. It also receives signed Checkout
Session webhooks through one shared HTTP endpoint.

Only a safe Checkout Session summary crosses the connector boundary: IDs,
status, amount, currency, expiry, URL, and application metadata. The connector
does not return or persist bank account details, customer payment methods, raw
provider bodies, API keys, or webhook signing secrets.

## Stripe setup

Choose **API key only** for applications that call Stripe operations without
receiving webhooks. Create a restricted key in Stripe Dashboard > Developers >
API keys with access to the operations the application uses. For
`GetPaymentIntent` and `CreateRefund`, grant PaymentIntent, Charge, and Refund
read access plus Refund write access. Store the `rk_test_` or `rk_live_` key as
`secret_key`; this method requires no webhook endpoint or signing secret.

Choose **API key and webhook** when using `checkoutSessionUpdated`:

1. Create a restricted or secret Stripe API key that can create and read
   Checkout Sessions.
2. Create one Stripe webhook endpoint that routes to this application's
   `CheckoutSessionWebhookHandler` path.
3. Subscribe it to:
   - `checkout.session.completed`
   - `checkout.session.async_payment_succeeded`
   - `checkout.session.async_payment_failed`
   - `checkout.session.expired`
4. Store the API key as `secret_key` and the endpoint signing secret as
   `webhook_secret`. Keep both in the same Stripe mode. Never place either
   secret in Flow input or connector configuration.

Existing connections without an `auth_method` keep using the webhook method.
A webhook request on an API-key-only connection fails closed because it has no
signing secret.

`checkout.session.completed` means the customer finished hosted Checkout; it
does not by itself prove that an ACH payment settled. Applications should issue
the paid ticket only after `checkout.session.async_payment_succeeded`, or after
`GetCheckoutSession` confirms `paymentStatus == "paid"` during reconciliation.

## Project configuration

Dex Web or Superverse Studio writes the connection and its Trigger binding to
the project configuration. The connection record and the binding record look
like this:

```json
{
  "connectorId": "stripe",
  "connectionName": "stripe-payments",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/stripe",
  "provider": "stripe",
  "configuration": {}
}
```

```json
{
  "connectorId": "stripe",
  "connectionName": "stripe-payments",
  "triggerName": "checkoutSessionUpdated",
  "bindingName": "registration-payments",
  "configuration": {
    "eventTypes": [
      "checkout.session.completed",
      "checkout.session.async_payment_succeeded",
      "checkout.session.async_payment_failed",
      "checkout.session.expired"
    ]
  }
}
```

The connection's selected-method credentials stay in private project storage
and are resolved for every call. The application reads
the configuration through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading)
and opens the connection with `stripe.NewProjectConnection`. Use
`NewProjectCheckoutSessionWebhookRuntime` when several bindings share the same
Stripe endpoint. The runtime gives every binding its own durable project inbox,
activates every Trigger, and exposes an `http.Handler`. Each connection has its
own endpoint, built on `sdkgo/webhooktrigger`, so a request verified with one
connection's signing secret reaches only that connection's bindings. The
[webhook receiver example](examples/webhook-receiver/main.go) wires it like
this:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
runtime, err := stripe.NewProjectCheckoutSessionWebhookRuntime(
	project,
	connectionName,
	[]stripe.ProjectCheckoutSessionUpdatedTriggerRoute{{BindingName: bindingName, Target: checkoutEventLogger{}}},
)
if err != nil {
	return err
}
mux := http.NewServeMux()
mux.Handle("/webhooks/stripe", runtime)
```

The HTTP handler verifies the signature against the exact bounded request body.
For a configured event, it persists the event in each matching durable inbox,
queues it for ordered delivery, and only then returns HTTP 200. A temporary
inbox or queue failure returns HTTP 503 so Stripe can retry. Target delivery
continues independently with bounded backoff. Invalid signatures and payloads
return HTTP 400. Unsupported valid Stripe events are acknowledged and ignored.

Applications must still deduplicate provider event IDs in durable Flow state.
Stripe and the connector both provide at-least-once delivery, so a process can
receive the same event again after an acknowledgement race.

## Operations

`CreateCardCheckoutSession` sends `mode=payment` and
`payment_method_types[0]=card`. `CreateACHCheckoutSession` sends
`mode=payment` and `payment_method_types[0]=us_bank_account`. It maps the stable Dex Call ID to
Stripe's `Idempotency-Key`, includes application metadata on both the Checkout
Session and PaymentIntent, and disables HTTP redirects so credentials cannot be
forwarded to another host.

The application supplies:

- a non-sensitive client reference ID;
- one plain customer email address;
- HTTPS success and cancel URLs;
- a lowercase three-letter currency and positive minor-unit amount;
- a product name and, optionally, bounded non-sensitive metadata.

Do not put personal, bank, card, or other sensitive data in Stripe metadata.

Create branches are `created`, `providerRejected`, `uncertain`,
`invalidResponse`, and `defect`. Transport failures, unreadable responses,
rate limits, and retryable 502/503/504 responses retry with the same
idempotency key. Stripe caches a 500 response for that key; a 500 or a
non-retryable or replayed 5xx selects `uncertain` because the Session cannot
be confirmed. An oversized response selects `invalidResponse`; increase
`maxResponseBytes` if the expected provider response exceeds the limit.

`GetCheckoutSession` returns `found`, `notFound`, `providerRejected`,
`invalidResponse`, or `defect`. Transport, rate-limit, and availability errors
retry under the generated Dex policy.

`GetPaymentIntent` reads one PaymentIntent (`pi_...`) with its latest charge
expanded and returns `id`, `amount` and `currency` (minor units),
`status`, `amountRefunded` (the latest charge's refunded amount, `0` without a
charge), `customerEmail` (the receipt email, otherwise the charge's billing
email) and `latestChargeId`. Branches are `found`, `notFound` (HTTP 404, for
example `resource_missing`), `providerRejected`, `invalidResponse`, and
`defect`.

`CreateRefund` posts `payment_intent`, `amount` (a positive minor-unit
amount), an optional `reason` (`duplicate`, `fraudulent`, or
`requested_by_customer`) and bounded `metadata` to `/v1/refunds`, with the
Step's stable Call ID as Stripe's `Idempotency-Key` and as the reserved
`metadata[dex_call_id]` marker. Applications can supply at most 49 additional
metadata fields and cannot use the `dex_call_id` key. Its `Refund` output has
`refundId`, `status` (`succeeded`, `pending`, `failed`, `requires_action`, or
`canceled`), `amount` and `currency`; read `status` on the `refunded` branch,
because a refund can be accepted without having succeeded. Branches are
`refunded`, `providerRejected`, `uncertain`, `invalidResponse`, and `defect`.
A Stripe 4xx error such as `charge_already_refunded` or an amount above the
refundable balance is `providerRejected`, and its failure message carries
Stripe's error code and message (never for 401 or 403, whose messages can echo
part of the key). Network losses, unreadable responses, timeouts, rate limits,
a 409 for a key still in flight, and retryable 502/503/504 responses retry with
the same key. A 500 or non-retryable or replayed 5xx prompts a bounded refund
list lookup filtered by PaymentIntent and matched on `dex_call_id`. A match
returns `refunded`; no match or an inconclusive lookup selects `uncertain`.
Transient read-back failures retry. An oversized refund creation response
selects `invalidResponse`; increase `maxResponseBytes` when the expected
response exceeds the limit. Reconcile uncertain outcomes before starting a
separate request with a new key.

The key needs PaymentIntent and Charge read access plus Refund read and write
access in addition to the Checkout Session permissions.

The `stripemock` package, generated from the manifest, scripts every
operation and branch in application tests without calling Stripe.

## Example

[`examples/create-checkout-session`](examples/create-checkout-session) is the
runnable ACH Checkout Flow. [`examples/create-card-checkout-session`](examples/create-card-checkout-session)
is the runnable card Checkout Flow. Each example has its own Worker and Flow source.

[`examples/refund-payment`](examples/refund-payment) is a runnable Start Flow
that reads a PaymentIntent with `GetPaymentIntent` and refunds the balance that
is still unrefunded with `CreateRefund`.

[`examples/webhook-receiver`](examples/webhook-receiver) is a runnable local
receiver that loads the project configuration, starts the durable Trigger
runtime, and exposes `/webhooks/stripe` without logging event payloads.

No live Stripe credentials are required by the deterministic test suite. The
suite uses a fake provider and signed webhook fixtures; a live Stripe account
and public HTTPS callback remain an operator-owned end-to-end check.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```
