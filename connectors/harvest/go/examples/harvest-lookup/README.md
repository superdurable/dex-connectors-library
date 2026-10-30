# Harvest lookup example

This Worker registers `HarvestLookup` with static connection name `harvest`. It calls `listProjects and listTimeEntries` in one typed Flow.

## Generate and run

From `connectors/harvest`, generate strict FDG 2.0 from the checked-in Flow source:

```bash
mkdir -p /tmp/harvest-lookup-fdg
dexcli visualize ./examples/harvest-lookup/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/harvest-lookup-fdg
```

The command must report `valid: true`. A local module may also report `connector_release_required` until the first release. From the repository root, create local release metadata for Dex Web Connections:

```bash
mkdir -p /tmp/harvest-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/harvest/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/harvest/go \
  --version v0.2.0 --tag connectors/harvest/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/harvest-release/connector-release.json \
  --digest-output /tmp/harvest-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/harvest-lookup-fdg \
  --connector-release-override harvest=/tmp/harvest-release
```

Configure the static `harvest` connection in Dex Web, then in a second terminal run `GOWORK=off go run ./examples/harvest-lookup` from `connectors/harvest` with the project environment described in [`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md). Start `HarvestLookup` through Dex Web's Start Flow form.

The deterministic mapping unit test runs with `GOWORK=off go test ./examples/harvest-lookup/...`. CI runs `flow/workflow_mock_integration_test.go` against a real Dex Worker and Client; local integration is left to CI.
