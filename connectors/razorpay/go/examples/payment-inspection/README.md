# Razorpay payment inspection Flow

This runnable Worker registers `RazorpayPaymentInspection`, `RazorpayCreateOrder`, `RazorpayCreateNormalRefund`, and `RazorpayCreatePaymentLink` through the static `razorpay` connection. These Flows persist only safe payment, order, refund, or payment-link metadata and expose read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/razorpay/go`, run:

```bash
dexcli visualize ./examples/payment-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/razorpay-payment-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override razorpay=<artifact-directory>`. In Dex Web Connections select the `razorpay` connection. Follow the [connection guide](../../../README.md) to create a Key ID and Key Secret and enter them in secret `key_id` and `key_secret`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/razorpay/go`:

```bash
GOWORK=off go run ./examples/payment-inspection
```

Start `RazorpayPaymentInspection` with a unique Flow ID and input `{"paymentId":"pay_DG4ZdRK8ZnXC3k"}`. Replace the sample with a payment ID returned by Razorpay. The Worker connects to Dex Server without an inbound listener.

Start `RazorpayCreateOrder` with a unique Flow ID and input `{"amount":5000,"currencyCode":"INR"}`. The amount is in currency subunits. The connector derives a unique receipt from the Dex Call ID and returns it with the created order; no customer data enters the Flow.

Start `RazorpayCreateNormalRefund` with a unique Flow ID and input `{"paymentId":"pay_DG4ZdRK8ZnXC3k","amount":100}`. Use a captured payment and specify the refund amount in currency subunits. Razorpay applies the stable Dex Call ID as its refund idempotency key.

Start `RazorpayCreatePaymentLink` with a unique Flow ID and input `{"amount":1000,"currencyCode":"INR"}`. The amount is in currency subunits; the connector derives a unique reference and creates a hosted link without sending notifications.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Razorpay authorization and retrieval remain unverified.
