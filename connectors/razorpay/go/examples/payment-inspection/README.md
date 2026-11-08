# Razorpay payment inspection Flow

This runnable Flow calls `getPayment` through the static `razorpay` connection. It persists the selected payment status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

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

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Razorpay authorization and retrieval remain unverified.
