# Datadog lookup example

This runnable Flow calls `datadog.NewQueryMetricsStep` with the static connection name `datadog` and completes with the typed `MetricsQuery` value. Optional branches are unwired and fail the Flow if selected. Its [mock-backed integration test](flow/workflow_mock_integration_test.go) uses a real Dex Worker and Client in CI.

## Generate the strict Flow Definition

From `connectors/datadog`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/datadog-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/datadog-lookup
```

The definition must report `valid: true`. The local module can warn that a connector release is required.

## Configure and run

From the repository root, create release metadata for the local source:

```bash
mkdir -p /tmp/datadog-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/datadog/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/datadog/go \
  --version v0.2.0 --tag connectors/datadog/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/datadog-release/connector-release.json \
  --digest-output /tmp/datadog-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/datadog/go/build" --connector-release-override datadog=/tmp/datadog-release
```

In Dex Web **Connections**, select `datadog / datadog`, follow the authorization guide beginning at [https://app.datadoghq.com/organization-settings/api-keys](https://app.datadoghq.com/organization-settings/api-keys), and save the connection. Keep keys in secret fields only. From `connectors/datadog`, run `go run ./examples/datadog-lookup` with the `DEX_PROJECT_*` environment described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading). In Dex Web **Start Flow**, select `DatadogLookup` and enter this JSON request:

```json
{
  "from": 1793145600,
  "to": 1793149200,
  "query": "system.cpu.user{*}"
}
```

The sample window is October 28, 2026, 00:00–01:00 UTC. Replace `from` and `to` with a recent window in Unix seconds when running against Datadog. The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live provider authorization and the real Dex example have not been exercised locally; CI runs the mock-backed integration test.
