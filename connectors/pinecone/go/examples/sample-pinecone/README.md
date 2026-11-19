# Pinecone sample Flow

The Worker registers two Flows on the static `pinecone` Dex Web connection. `PineconeSample` calls `upsertVectors`; `PineconeVectorFetch` calls `fetchVectors`. Start the latter with `{"namespace":"example","ids":["vec-meridian"]}` to read dense values without metadata. An unwired optional provider branch fails its Flow.

Generate strict FDG 2.0 from this source:

```bash
mkdir -p build
dexcli visualize ./examples/sample-pinecone/flow/workflow.go --schema-version 2.0 --json --out ./build/sample-pinecone
dexcli visualize ./examples/sample-pinecone/flow/vector_fetch.go --schema-version 2.0 --lint app --json --out ./build/vector-fetch
```

The command must report `valid: true`. From the repository root, build the local release metadata for Dex Web:

```bash
mkdir -p /tmp/pinecone-release
go run ./cmd/connectorctl release-artifact --manifest connectors/pinecone/connector.yaml --module-path github.com/superdurable/dex-connectors-library/connectors/pinecone/go --version v0.3.0 --tag connectors/pinecone/go/v0.3.0 --source-sha "$(git rev-parse HEAD)" --output /tmp/pinecone-release/connector-release.json --digest-output /tmp/pinecone-release/connector-release.json.sha256
dexcli dev --flow-rendering-dir "$PWD/connectors/pinecone/go/build" --connector-release-override pinecone=/tmp/pinecone-release
```

In Dex Web Connections, create `pinecone / pinecone`, follow the connector's authorization guide, and save. With the project's `DEX_PROJECT_*` environment set, start the Worker from `connectors/pinecone` using `go run ./examples/sample-pinecone`. In Dex Web Start Flow, select `PineconeSample` and the Worker, and enter an input matching `UpsertVectorsInput`. For a start input, use a real ID from your provider in place of each sample ID:

```json
{"namespace":"example","vectors":[{"id":"vec-meridian","values":[0.1,0.2]}]}
```

Vector metadata in this example accepts string values, so Dex Web can render its start form. The mock-backed real-Dex integration test runs in CI.
