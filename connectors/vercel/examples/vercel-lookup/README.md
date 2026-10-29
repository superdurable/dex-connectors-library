# Vercel lookup example

This runnable Flow calls `vercel.NewListDeploymentsStep` with the static connection name `vercel` and completes with the typed `DeploymentsPage` value. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client in CI.

## Generate the strict Flow Definition

From `connectors/vercel`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/vercel-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/vercel-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/vercel-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/vercel/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/vercel \
  --version v0.1.0 --tag connectors/vercel/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/vercel-release/connector-release.json \
  --digest-output /tmp/vercel-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/vercel/build" --connector-release-override vercel=/tmp/vercel-release
```

In Dex Web **Connections**, select `vercel / vercel`, follow the authorization guide beginning at [https://vercel.com/account/tokens](https://vercel.com/account/tokens), and save the connection. Keep keys in secret fields only. From `connectors/vercel`, run `go run ./examples/vercel-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `VercelLookup` and enter this JSON request:

```json
{}
```

Use the input type `ListDeploymentsInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../README.md). The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization and the real Dex example have not been exercised locally; CI runs the mock-backed integration test.
