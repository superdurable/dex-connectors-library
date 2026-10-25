# Stripe refund-payment example

This example refunds the unrefunded balance of one Stripe PaymentIntent in a
Flow started from Dex Web **Start Flow**. It reads the PaymentIntent with
`getPaymentIntent`, then calls `createRefund` for `amount - amountRefunded`
when the payment succeeded. It completes without a refund when nothing remains.

Generate strict FDG 2.0 from `connectors/stripe`:

```bash
mkdir -p build
dexcli visualize ./examples/refund-payment/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/refund-payment
```

Run `dexcli dev` with that build directory and configure
`stripe / stripe-payments` under **Connections**. The Stripe key needs
PaymentIntent and Charge read access and Refund write access. Start the Worker
with the `DEX_PROJECT_*` environment that names the connection, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
go run ./examples/refund-payment
```

Start `StripeRefundPayment` with a unique Flow ID:

```json
{
  "paymentIntentId":"pi_3Pabc123",
  "reason":"requested_by_customer"
}
```

Only `found` and `refunded` are wired. A missing PaymentIntent, a Stripe
rejection, an invalid response, local defects, and exhausted keyed retries fail
the Flow. Reconcile a refund after retry exhaustion before starting another
request with a new key.

```bash
GOWORK=off go test -race ./examples/refund-payment/...
```
