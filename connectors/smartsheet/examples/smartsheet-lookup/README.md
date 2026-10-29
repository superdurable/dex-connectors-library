# Smartsheet lookup example

This Worker registers `SmartsheetLookup` with static connection name `smartsheet`. It calls `listSheets and getSheet` in one typed Flow.

## Generate and run

From `connectors/smartsheet`, generate strict FDG 2.0 from the checked-in Flow source:

```bash
mkdir -p /tmp/smartsheet-lookup-fdg
dexcli visualize ./examples/smartsheet-lookup/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/smartsheet-lookup-fdg
```

The command must report `valid: true`. A local module may also report `connector_release_required` until the first release. From the repository root, create local release metadata for Dex Web Connections:

```bash
mkdir -p /tmp/smartsheet-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/smartsheet/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/smartsheet \
  --version v0.1.0 --tag connectors/smartsheet/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/smartsheet-release/connector-release.json \
  --digest-output /tmp/smartsheet-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/smartsheet-lookup-fdg \
  --connector-release-override smartsheet=/tmp/smartsheet-release
```

Configure the static `smartsheet` connection in Dex Web, then in a second terminal run `GOWORK=off go run ./examples/smartsheet-lookup` from `connectors/smartsheet` with the project environment described in [`sdkgo/projectconfig`](../../../../sdkgo/projectconfig/README.md). Start `SmartsheetLookup` through Dex Web's Start Flow form.

The deterministic mapping unit test runs with `GOWORK=off go test ./examples/smartsheet-lookup/...`. CI runs `flow/workflow_mock_integration_test.go` against a real Dex Worker and Client; local integration is left to CI.
