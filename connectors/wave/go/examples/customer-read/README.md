# Wave customer lookup Flow

This runnable Flow reads one Wave customer with `getCustomer` through the static `wave` connection and persists its profile. Configure secret `accessToken` in Dex Web Connections using the [connection guide](../../../README.md). Optional branches are unwired and fail the Flow when selected.

`WaveCreateCustomer` runs `createCustomer` with the same static connection. Start it with `{"businessId":"business-1","name":"Example Customer"}` after replacing the business ID with one from the Wave API Playground. The mutation has a three-send cap and can fail with an uncertain outcome. Its mock-backed real Dex test verifies one customer creation call and the returned profile.

`WaveCreateProduct` runs `createProduct`. Start it with `{"businessId":"business-1","name":"Example Product","unitPrice":"2.50","incomeAccountId":"account-1"}` after replacing both IDs with values from your Wave business. Find a sold product's income account in the API Playground using the [official product creation guide](https://developer.waveapps.com/hc/en-us/articles/360033284492-Mutation-Create-product-service). The mutation has a three-send cap; its mock-backed real Dex test verifies the returned product.

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
