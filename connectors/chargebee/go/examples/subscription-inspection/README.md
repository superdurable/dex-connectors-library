# Chargebee subscription inspection Flow

This runnable Flow calls `getSubscription` through the static `chargebee` connection. It persists the selected subscription status in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/chargebee/go`, run:

```bash
dexcli visualize ./examples/subscription-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/chargebee-subscription-inspection-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override chargebee=<artifact-directory>`. In Dex Web Connections select the `chargebee` connection. Follow the [connection guide](../../../README.md) to create a Read-Only API key and enter it in secret `api_key`. Set `baseUrl` to the matching Test or Live site API URL. Leave `timeout` and `maxResponseBytes` blank for displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/chargebee/go`:

```bash
GOWORK=off go run ./examples/subscription-inspection
```

Start `ChargebeeSubscriptionInspection` with a unique Flow ID and input `{"subscriptionId":"sub_123456"}`. Replace the sample with a subscription ID returned by Chargebee. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Chargebee authorization and retrieval remain unverified.
