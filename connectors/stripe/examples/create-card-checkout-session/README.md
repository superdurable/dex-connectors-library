# Stripe create-card-checkout-session example

This example creates one hosted Stripe card Checkout Session in a Flow started
from Dex Web **Start Flow**.

Generate strict FDG 2.0 from `connectors/stripe`:

```bash
mkdir -p build
dexcli visualize ./examples/create-card-checkout-session/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/create-card-checkout-session
```

Run `dexcli dev` with that build directory and configure
`stripe / stripe-payments` under **Connections**. Dex Web or Superverse Studio
writes the connection to the project configuration. Start the Worker with the
`DEX_PROJECT_*` environment that names it:

```bash
go run ./examples/create-card-checkout-session
```

Start `CardFlow` with a unique Flow ID:

```json
{
  "clientReferenceId":"registration-42",
  "customerEmail":"person@example.com",
  "successUrl":"https://example.com/success",
  "cancelUrl":"https://example.com/cancel",
  "currency":"usd",
  "unitAmount":7500,
  "productName":"BBQ ticket"
}
```

Only `created` is wired. Provider rejection, uncertainty after a Stripe 500,
an invalid response, local defects, and exhausted keyed retries fail the Flow.
The returned safe Checkout summary contains the hosted URL for the customer to
open.

```bash
GOWORK=off go test ./examples/create-card-checkout-session/...
```
