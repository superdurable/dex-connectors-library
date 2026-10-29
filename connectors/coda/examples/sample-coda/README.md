# Coda sample Flow

The runnable Flow in [flow/workflow.go](flow/workflow.go) calls `listRows` through the static `coda` Dex Web connection and completes with its typed result. An unwired optional provider branch fails the Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-coda/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-coda
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/coda-release
go run ./cmd/connectorctl release-artifact --manifest connectors/coda/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/coda --version v0.1.0 --tag connectors/coda/v0.1.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/coda-release/connector-release.json --digest-output /tmp/coda-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/coda/build" --connector-release-override coda=/tmp/coda-release
```

In Dex Web Connections, create `coda / coda`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/coda` using `go run ./examples/sample-coda`. In Dex Web Start Flow, select `CodaSample` and the Worker, and enter an input matching `ListRowsInput`. For a start input, use a real ID from your provider in place of each sample ID:

```json
{"docId":"AbCDeFGH","tableId":"grid-pqRst-U","limit":10}
```

The mock-backed real-Dex integration test runs in CI.
