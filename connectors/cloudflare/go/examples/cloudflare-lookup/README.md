# Cloudflare lookup example

This runnable Flow calls `cloudflare.NewListZonesStep` and Go `cloudflare.NewListDNSRecordsStep` with the static connection name `cloudflare`. It completes with the typed zone and DNS record pages. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client.

The same Worker also registers `CloudflareCreateDNSRecord`. Start it with a `CreateDNSRecordInput` request containing `zoneId`, `type`, `name`, `content`, `ttl` (seconds or 1 for automatic), and `proxied`. This Flow creates a real DNS record when connected to Cloudflare; check the target zone and name before starting it. The integration test exercises its default mock through a real Dex Worker and Client.

## Generate the strict Flow Definition

From `connectors/cloudflare/go`, use the latest stable dexcli:

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
  --module-path github.com/superdurable/dex-connectors-library/connectors/cloudflare/go \
  --version v0.3.0 --tag connectors/cloudflare/go/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/cloudflare-release/connector-release.json \
  --digest-output /tmp/cloudflare-release/connector-release.json.sha256
dexcli dev -open=false --flow-rendering-dir "$PWD/connectors/cloudflare/go/build" --connector-release-override cloudflare=/tmp/cloudflare-release
```

In Dex Web **Connections**, select `cloudflare / cloudflare`, follow the authorization guide beginning at [https://dash.cloudflare.com/profile/api-tokens](https://dash.cloudflare.com/profile/api-tokens), and save the connection. Keep keys in secret fields only. From `connectors/cloudflare/go`, run `GOWORK=off go run ./examples/cloudflare-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `CloudflareLookup` and enter this JSON request:

```json
{}
```

Use the input type `ListZonesInput` as the JSON shape; the provider-specific fields are described in the [connector README](../../../README.md). The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization has not been exercised. The mock-backed real Dex integration test runs from the Go module with the `integration` build tag.
