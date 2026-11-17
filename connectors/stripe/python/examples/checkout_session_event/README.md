# Stripe Checkout Session event Flow

This Python application serves signed `checkoutSessionUpdated` deliveries at
`/stripe/checkout-events`, stores the reduced Checkout Session snapshot in the
`stripe-checkout-event-python` Attribute, and completes one
`StripeCheckoutEventPython` Flow per Stripe event ID. A retry reuses the same
Flow. The `checkout-events` binding belongs to the static `stripe-payments`
connection and accepts four Checkout Session event types: `completed`,
`async_payment_succeeded`, `async_payment_failed`, and `expired`. Other event
types are acknowledged without a Flow. The binding's empty configuration
accepts all four supported types.

From `connectors/stripe/python`:

```sh
uv sync --locked
dexcli visualize examples/checkout_session_event/flow.py --language python \
  --schema-version 2.0 --lint app --json --out build/checkout-event \
  --python .venv/bin/python
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
uv run --with uvicorn python -m examples.checkout_session_event.main
```

The server listens on `STRIPE_BIND_ADDRESS` (default `127.0.0.1:8866`).
`GET /readyz` answers 200 when the binding is ready. Give Stripe a public HTTPS
URL ending in `/stripe/checkout-events`. In the Stripe Dashboard, choose the
matching Test or Live mode, open **Developers > Webhooks**, create or select the
endpoint, subscribe it to the four event types, then copy its `whsec_` signing
secret from **Signing secret > Reveal** into the connection's `webhook_secret`.
Use the **API key and webhook** connection method; `secret_key` is a separate
API credential. The [connector README](../../../README.md#project-configuration)
describes the Go webhook receiver and common signing contract.

The endpoint checks every raw body against `Stripe-Signature` and rejects an
out-of-window timestamp. A 200 means the event is durably stored before
acknowledgment; a missing secret or unready binding answers 503 so Stripe can
retry. The event ID keeps a redelivery on the same Flow even if Stripe signs
it with a new timestamp.

The integration test runs this exact Flow and Worker against a real Dex Server.
It acknowledges a signed event while the target is unavailable, restarts the
Trigger, replays the event into one Flow, and verifies redelivery:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
uv run pytest examples/checkout_session_event/integration_test.py
```

Live Stripe webhook delivery has not been verified.
