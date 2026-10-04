# Stripe webhook receiver example

This example runs the connector's durable Checkout Session Trigger without a
Dex application. It is useful for validating a project connection, Stripe
signature verification, and public webhook routing before wiring events to a
Flow.

1. Have Dex Web or Superverse Studio write the `stripe-payments` connection
   and its `registration-payments` binding, shown in the
   [connector README](../../README.md#project-configuration), to the project
   configuration.
2. Set the `DEX_PROJECT_*` environment that names that configuration, as
   [project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
   describes.
3. Run `go run ./examples/webhook-receiver` from `connectors/stripe`.
4. Expose `http://127.0.0.1:8080/webhooks/stripe` through an HTTPS development
   tunnel and configure that exact public URL in Stripe.
5. Send one subscribed event from Stripe's test mode.

Set `STRIPE_WEBHOOK_BIND_ADDRESS` to change the listener. The example logs only
event, session, and client-reference IDs; it does not log webhook bodies,
metadata, customer details, API keys, or signing secrets.

This target only logs normalized events. A production application replaces it
with a Dex Flow or RPC target, owns event-ID deduplication, and issues tickets
only after ACH settlement succeeds.
