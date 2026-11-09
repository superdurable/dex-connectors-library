# Customer.io customer lookup Flow

This runnable Flow calls `getCustomersByEmail` through the static `customerio` connection. It persists matching profile IDs in an Attribute and exposes them through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/customerio/go`, run:

```bash
dexcli visualize ./examples/customer-lookup/flow/workflow.go --schema-version 2.0 --json --out /tmp/customerio-customer-lookup-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override customerio=<artifact-directory>`. In Dex Web Connections select the `customerio` connection. Follow the [connection guide](../../../README.md) to enter secret `privateKey`; leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/customerio/go`:

```bash
GOWORK=off go run ./examples/customer-lookup
```

Start `CustomerioCustomerLookup` with a unique Flow ID and input `{"email":"person@example.com"}`. Replace the sample with an email from the Customer.io People page. The Worker connects to Dex Server without an inbound listener.

CI runs the fixture-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Customer.io authorization and lookup remain unverified.
