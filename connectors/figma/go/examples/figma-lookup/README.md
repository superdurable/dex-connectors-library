# Figma lookup example

This Worker registers `FigmaLookup` with static connection name `figma`. It calls `getFile and getFileComments` in one typed Flow.

## Generate and run

From `connectors/figma`, generate strict FDG 2.0 from the checked-in Flow source:

```bash
mkdir -p /tmp/figma-lookup-fdg
dexcli visualize ./examples/figma-lookup/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/figma-lookup-fdg
```

The command must report `valid: true`. A local module may also report `connector_release_required` until the first release. From the repository root, create local release metadata for Dex Web Connections:

```bash
mkdir -p /tmp/figma-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/figma/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/figma/go \
  --version v0.2.0 --tag connectors/figma/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/figma-release/connector-release.json \
  --digest-output /tmp/figma-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/figma-lookup-fdg \
  --connector-release-override figma=/tmp/figma-release
```

Configure the static `figma` connection in Dex Web, then in a second terminal run `GOWORK=off go run ./examples/figma-lookup` from `connectors/figma` with the project environment described in [`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md). Start `FigmaLookup` through Dex Web's Start Flow form.

The deterministic mapping unit test runs with `GOWORK=off go test ./examples/figma-lookup/...`. CI runs `flow/workflow_mock_integration_test.go` against a real Dex Worker and Client; local integration is left to CI.
