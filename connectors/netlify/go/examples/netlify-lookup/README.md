# Netlify lookup example

This runnable Flow calls `netlify.NewListSitesStep` with the static connection name `netlify` and completes with the typed `SitesPage` value. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client in CI.

## Generate the strict Flow Definition

From `connectors/netlify`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/netlify-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/netlify-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/netlify-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/netlify/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/netlify/go \
  --version v0.2.0 --tag connectors/netlify/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/netlify-release/connector-release.json \
  --digest-output /tmp/netlify-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/netlify/go/build" --connector-release-override netlify=/tmp/netlify-release
```

In Dex Web **Connections**, select `netlify / netlify`, follow the authorization guide beginning at [https://app.netlify.com/user/applications#personal-access-tokens](https://app.netlify.com/user/applications#personal-access-tokens), and save the connection. Keep keys in secret fields only. From `connectors/netlify`, run `go run ./examples/netlify-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `NetlifyLookup` and enter this JSON request:

```json
{}
```

Use the input type `ListSitesInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../../README.md). The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization and the real Dex example have not been exercised locally; CI runs the mock-backed integration test.
