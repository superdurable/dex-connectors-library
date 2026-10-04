# Stripe create-checkout-session example

This example creates one hosted Stripe ACH Checkout Session in a Flow started
from Dex Web **Start Flow**.

Generate strict FDG 2.0 from `connectors/stripe`:

```bash
mkdir -p build
dexcli visualize ./examples/create-checkout-session/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/create-checkout-session
```

Run `dexcli dev` with that build directory and configure
`stripe / stripe-payments` under **Connections**. Dex Web or Superverse Studio
writes the connection to the project configuration; start the Worker with the
`DEX_PROJECT_*` environment that names it, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
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
