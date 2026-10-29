# Cloudflare lookup example

This runnable Flow calls `cloudflare.NewListZonesStep` with the static connection name `cloudflare` and completes with the typed `ZonesPage` value. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client in CI.

## Generate the strict Flow Definition

From `connectors/cloudflare`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/cloudflare-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/cloudflare-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/cloudflare-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/cloudflare/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/cloudflare \
  --version v0.1.0 --tag connectors/cloudflare/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/cloudflare-release/connector-release.json \
  --digest-output /tmp/cloudflare-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/cloudflare/build" --connector-release-override cloudflare=/tmp/cloudflare-release
```

In Dex Web **Connections**, select `cloudflare / cloudflare`, follow the authorization guide beginning at [https://dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens), and save the connection. Keep keys in secret fields only. From `connectors/cloudflare`, run `go run ./examples/cloudflare-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `CloudflareLookup` and enter this JSON request:

```json
{}
```

Use the input type `ListZonesInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../README.md). The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization and the real Dex example have not been exercised locally; CI runs the mock-backed integration test.
