# Productboard feature discovery Flow

This runnable Flow calls `listFeatures` through the static `productboard` connection. It saves the selected page in an Attribute and exposes it through read-only Dex Web RPCs. Optional branches are unwired and fail the Flow when selected.

## Strict FDG 2.0 generation

From `connectors/productboard/go`, run:

```bash
dexcli visualize ./examples/feature-discovery/flow/workflow.go --schema-version 2.0 --json --out /tmp/productboard-feature-discovery-fdg
```

The command must report `valid: true`. An unpublished connector may report `connector_release_required` until release metadata exists.

## Run with Dex Web

Build the connector release artifact, then run `dexcli dev` with `--connector-release-override productboard=<artifact-directory>`. In Dex Web Connections select the `productboard` connection. Create a Personal Access Token at [Workspace Settings > Integrations > Public APIs > Access Token](https://developer.productboard.com/reference/api-token) and enter it in the secret `api_token` field. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults.

With the project's `DEX_PROJECT_*` environment set, run from `connectors/productboard/go`:

```bash
GOWORK=off go run ./examples/feature-discovery
```

Start `ProductboardFeatureDiscovery` with a unique Flow ID and input `{"pageCursor":""}`. Copy the returned `nextCursor` into `pageCursor` for another page. The Worker connects to Dex Server without an inbound listener.

CI runs the mock-backed real-Dex integration test. Local provider and deterministic Flow tests run with `GOWORK=off go test ./...`.
