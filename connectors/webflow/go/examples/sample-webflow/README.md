# Webflow sample Flow

The runnable Flow in [flow/workflow.go](flow/workflow.go) calls `listCollectionItems` through the static `webflow` Dex Web connection and completes with its typed result. An unwired optional provider branch fails the Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-webflow/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-webflow
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/webflow-release
go run ./cmd/connectorctl release-artifact --manifest connectors/webflow/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/webflow/go --version v0.2.0 --tag connectors/webflow/go/v0.2.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/webflow-release/connector-release.json --digest-output /tmp/webflow-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/webflow/go/build" --connector-release-override webflow=/tmp/webflow-release
```

In Dex Web Connections, create `webflow / webflow`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/webflow` using `go run ./examples/sample-webflow`. In Dex Web Start Flow, select `WebflowSample` and the Worker, and enter an input matching `ListCollectionItemsInput`. For a start input, use a real ID from your provider in place of each sample ID:

```json
{"collectionId":"62b720ef280c7a7a3be8cabe","limit":10}
```

The mock-backed real-Dex integration test runs in CI.
