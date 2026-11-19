# Deel contract inspection Flow

This worker registers `DeelContractInspection` and `DeelContractListing` through the static `deel` connection. The inspection Flow calls `getContract`; the listing Flow calls `listContracts`. Both persist their selected result in an Attribute and expose it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/deel/go`, run:

```bash
dexcli visualize ./examples/contract-inspection/flow/workflow.go --schema-version 2.0 --json --out /tmp/deel-contract-inspection-fdg
dexcli visualize ./examples/contract-inspection/flow/contract_listing.go --schema-version 2.0 --json --out /tmp/deel-contract-listing-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev -open=false` with `--connector-release-override deel=<artifact-directory>`. In Dex Web Connections select the `deel` connection. Follow the [connection guide](../../../README.md) to create an organization API token and enter it in secret `api_token`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for Sandbox defaults; select the Production API URL for Production.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/deel/go`:

```bash
GOWORK=off go run ./examples/contract-inspection
```

Start `DeelContractInspection` with a unique Flow ID and input `{"contractId":"123e4567-e89b-12d3-a456-426614174000"}`. Replace the sample with a contract ID returned by Deel. The Worker connects to Dex Server without an inbound listener.

Start `DeelContractListing` with a unique Flow ID and input such as `{"limit":10}`. Use the returned `nextCursor` as `afterCursor` to request the next page.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`. Real Deel authorization and provider behavior remain unverified.
