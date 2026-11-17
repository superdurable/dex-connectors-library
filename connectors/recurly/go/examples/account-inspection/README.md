# Recurly account inspection Flow

This runnable Worker registers `RecurlyAccountInspection`, `RecurlyCreateAccount`, `RecurlyReactivateAccount`, `RecurlySubscriptionInspection`, `RecurlyCreatePlan`, and `RecurlyInvoiceInspection` through the static `recurly` connection. Each Flow persists only account, subscription, plan, or invoice identity in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/recurly/go`, run:

```bash
dexcli visualize ./examples/account-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/recurly-account-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override recurly=<artifact-directory>`. In Dex Web Connections select the `recurly` connection. Follow the [connection guide](../../../README.md) to create a Private API Key and enter it in secret `api_key`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/recurly/go`:

```bash
GOWORK=off go run ./examples/account-inspection
```

Start `RecurlyAccountInspection` with a unique Flow ID and input `{"accountId":"e28zov4fw0v2"}`. Replace the sample with an account ID returned by Recurly. The Worker connects to Dex Server without an inbound listener.

Start `RecurlyCreateAccount` with a unique Flow ID and input `{"codePrefix":"demo"}`. The connector appends a unique Dex Call marker to the prefix and returns the resulting account ID and code.

Start `RecurlySubscriptionInspection` with a unique Flow ID and input `{"subscriptionId":"e28zov4fw0v2"}`. Replace the sample with a subscription ID returned by Recurly. The Flow returns only the subscription ID and lifecycle state.

Start `RecurlyCreatePlan` with a unique Flow ID and input `{"codePrefix":"standard","name":"Standard","currencyCode":"USD","unitAmount":10.5,"intervalUnit":"months","intervalLength":1}`. `unitAmount` is in major currency units. The connector appends a unique Dex Call marker to the code prefix.

Start `RecurlyInvoiceInspection` with a unique Flow ID and input `{"invoiceId":"e28zov4fw0v2"}`. Replace the sample with an invoice ID returned by Recurly. The Flow returns only the invoice ID and state.

Start `RecurlyReactivateAccount` with a unique Flow ID and input `{"accountId":"e28zov4fw0v2"}`. Use an inactive account ID. Reactivation returns only account identity and state; it does not restore billing information.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Recurly authorization and retrieval remain unverified.
