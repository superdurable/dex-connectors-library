# Stripe Connector

This module creates and reconciles Stripe-hosted Checkout Sessions that accept
US bank account (ACH Direct Debit) payments. It also receives signed Checkout
Session webhooks through one shared HTTP endpoint.

Only a safe Checkout Session summary crosses the connector boundary: IDs,
status, amount, currency, expiry, URL, and application metadata. The connector
does not return or persist bank account details, customer payment methods, raw
provider bodies, API keys, or webhook signing secrets.

## Stripe setup

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
   `webhook_secret`. Never place either secret in Flow input or connector
   configuration.

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

The connection's credential, `secret_key` and `webhook_secret`, stays in the
private project storage and is resolved for every call. The application reads
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

`CreateACHCheckoutSession` always sends `mode=payment` and
`payment_method_types[0]=us_bank_account`. It maps the stable Dex Call ID to
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
`invalidResponse`, and `defect`. A transport or Stripe 5xx response after
dispatch is `uncertain`; reconcile it instead of starting an unrelated second
payment. Rate limits retry with the same idempotency key.

`GetCheckoutSession` returns `found`, `notFound`, `providerRejected`,
`invalidResponse`, or `defect`. Transport, rate-limit, and availability errors
retry under the generated Dex policy.

## Example

[`examples/create-checkout-session`](examples/create-checkout-session) is a
runnable Start Flow for creating one hosted ACH Checkout Session.

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
