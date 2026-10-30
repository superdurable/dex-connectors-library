# Algolia index search example

This runnable Flow searches an Algolia index from Dex Web Start Flow. The `searched` branch completes with a bounded hit page. Optional branches are unwired and fail the Flow. `ConnectionName` is statically `algolia`. Dex Web summary and display views show the completed provider outcome.

## Generate strict FDG 2.0

From `connectors/algolia` with the latest stable Dex CLI:

```bash
mkdir -p build
dexcli visualize ./examples/index-search/flow/workflow.go --schema-version 2.0 --json --out ./build/index-search
```

The result must report `valid: true`. Local source may report `connector_release_required` until published.

## Run

From the repository root, create a release override and run Dex:

```bash
mkdir -p /tmp/algolia-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/algolia/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/algolia/go \
  --version v0.1.0 --tag connectors/algolia/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/algolia-release/connector-release.json \
  --digest-output /tmp/algolia-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/algolia/build" --connector-release-override algolia=/tmp/algolia-release
```

Set the project environment described by [`projectconfig`](../../../../sdkgo/projectconfig/README.md), then start the Worker:

```bash
cd connectors/algolia
go run ./examples/index-search
```

Configure `algolia / algolia` in Dex Web Connections using the Application ID and restricted API key from [Algolia API Keys](https://www.algolia.com/account/api-keys/all). Find your index under **Search > Indices** in the Algolia dashboard; use its exact name in `indexName`. In Start Flow choose `AlgoliaIndexSearch`, enter a unique Flow ID, and submit:

```json
{"indexName":"products","query":"notebook"}
```

A blank query matches all records. The example requests up to ten hits.

## Tests

`GOWORK=off go test ./...` checks deterministic mapping. CI runs the generated-mock real-Dex test. Live Algolia behavior remains unverified.
