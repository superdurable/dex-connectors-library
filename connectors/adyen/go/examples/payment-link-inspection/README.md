# Adyen payment link inspection Flow

This runnable Flow calls `getPaymentLink` through the static `adyen` connection. It persists the selected payment link status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/adyen/go`, run:

```bash
dexcli visualize ./examples/payment-link-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/adyen-payment-link-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override adyen=<artifact-directory>`. In Dex Web Connections select the `adyen` connection. Follow the [connection guide](../../../README.md) to create a server-side API key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Sandbox defaults; select the Live API URL for Live.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/adyen/go`:

```bash
GOWORK=off go run ./examples/payment-link-inspection
```

Start `AdyenPaymentLinkInspection` with a unique Flow ID and input `{"paymentLinkId":"PL50C5F751CED39G71"}`. Replace the sample with a payment link ID returned by Adyen. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Adyen authorization and retrieval remain unverified.

## Payment link expiry

The same Worker registers `AdyenExpirePaymentLink`. Start it with `ExpirePaymentLinkInput{PaymentLinkID: "PL50C5F751CED39G71"}` to force a link to `expired`. The checked-in mock-backed integration test exercises the mutation through a real Dex Worker and Client.

## Payment link creation

The Worker also registers `AdyenCreatePaymentLink`. Start it with `CreatePaymentLinkInput{MerchantAccount: "TestMerchant", Reference: "order-100", CurrencyCode: "EUR", AmountMinorUnits: 1200, CountryCode: "NL"}` after replacing the merchant account and reference. Its result contains the shopper-facing URL, which should be shared only with the intended shopper. The checked-in mock-backed integration test exercises this Flow through a real Dex Worker and Client.

## Payment method availability

The Worker also registers `AdyenListPaymentMethods`. Start it with `ListPaymentMethodsInput{MerchantAccount: "TestMerchant", CurrencyCode: "EUR", AmountMinorUnits: 1200, CountryCode: "NL"}` after replacing the merchant account and amount. Its result includes only method codes and names; stored shopper payment methods are discarded. The checked-in mock-backed integration test exercises this Query through a real Dex Worker and Client.

## Payment cancellation

The Worker also registers `AdyenCancelPayment`. Start it with the `PaymentReferenceModificationInput` fields shown in `flow/cancel_payment_mock_integration_test.go`, replacing the merchant account and PSP reference with those from your Customer Area. The result is a request receipt; inspect the CANCELLATION webhook for the final outcome. The checked-in integration test exercises the Flow through a real Dex Worker and Client using a mock provider.
