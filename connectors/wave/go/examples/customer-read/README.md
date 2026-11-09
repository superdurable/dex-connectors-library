# Wave customer lookup Flow

This runnable Flow reads one Wave customer with `getCustomer` through the static `wave` connection and persists its profile. Configure secret `accessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/wave/go`, run:

```bash
dexcli visualize ./examples/customer-read/flow/workflow.go --schema-version 2.0 --json --out /tmp/wave-customer-read-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override wave=<artifact-directory>`. In Dex Web Connections select the `wave` connection. Follow the [connection guide](../../../README.md) to enter secret `accessToken`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/wave/go`:

```bash
GOWORK=off go run ./examples/customer-read
```

Start `WaveCustomerRead` with a unique Flow ID and input `{"businessId":"business-1","customerId":"customer-1"}`. Replace the sample IDs with IDs from your Wave business. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Wave authorization and lookup remain unverified.
