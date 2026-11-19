# Contentful sample Flow

The runnable Flows in [flow/workflow.go](flow/workflow.go) and [flow/assets.go](flow/assets.go) call `getEntry` and `getAsset` through the static `contentful` Dex Web connection and complete with typed results. An unwired optional provider branch fails a Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-contentful/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-contentful
dexcli visualize ./examples/sample-contentful/flow/assets.go --schema-version 2.0 --lint app --json --out ./build/sample-contentful-asset
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/contentful-release
go run ./cmd/connectorctl release-artifact --manifest connectors/contentful/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/contentful/go --version v0.3.0 --tag connectors/contentful/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/contentful-release/connector-release.json --digest-output /tmp/contentful-release/connector-release.json.sha256
dexcli dev -open=false --flow-rendering-dir "$PWD/connectors/contentful/go/build" --connector-release-override contentful=/tmp/contentful-release
```

In Dex Web Connections, create `contentful / contentful`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/contentful` using `go run ./examples/sample-contentful`. In Dex Web Start Flow, select `ContentfulSample` and the Worker, and enter an input matching `GetEntryInput`. For a start input, use a real ID from your provider in place of each sample ID:

```json
{"spaceId":"space123","environmentId":"master","entryId":"entry-meridian"}
```

Select `ContentfulAssetInspection` to inspect an asset with `{"spaceId":"space123","environmentId":"master","assetId":"asset-meridian"}`. Replace all sample IDs with IDs from the selected space and environment.

The mock-backed real-Dex integration test runs in CI.
