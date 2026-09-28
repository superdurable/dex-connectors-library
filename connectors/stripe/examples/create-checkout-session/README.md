# Stripe create-checkout-session example

This example creates one hosted Stripe ACH Checkout Session in a Flow started
from Dex Web **Start Flow**.

Generate strict FDG 2.0 from `connectors/stripe`:

```bash
mkdir -p build
dexcli visualize ./examples/create-checkout-session/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/create-checkout-session
```

Run `dexcli dev` with that build directory, configure
`stripe / stripe-payments` under **Connections**, then start the Worker:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/create-checkout-session
```

Start `StripeCreateCheckoutSession` with a unique Flow ID:

```json
{
  "clientReferenceId":"registration-42",
  "customerEmail":"person@example.com",
  "successUrl":"https://example.com/success",
  "cancelUrl":"https://example.com/cancel",
  "currency":"usd",
  "unitAmount":7500,
  "productName":"Conference ticket"
}
```

Only `created` is wired. Provider rejection, an uncertain dispatch, an invalid
response, and local defects fail the Flow. The webhook receiver remains a
separate example because delayed ACH settlement is a Trigger concern.

```bash
GOWORK=off go test -race ./examples/create-checkout-session/...
```
