# OpenSearch document index Flow

This runnable Flow calls `indexDocument` with the static `opensearch` connection and completes with the index and document ID. The operation fully replaces the document at that ID. Optional branches are unwired, so provider rejection fails the Flow.

## Strict FDG 2.0 generation

From `connectors/opensearch/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/document-index/flow/workflow.go --schema-version 2.0 --json --out ./build/document-index
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override opensearch=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `opensearch / opensearch`. Use a dedicated internal user with `indices:data/write/index` permission for the target index; see [OpenSearch role permissions](https://docs.opensearch.org/latest/security/access-control/permissions/). Set the HTTPS cluster REST endpoint, username, and secret password. Leave the safety limits blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/opensearch/go`:

```bash
go run ./examples/document-index
```

In Dex Web Start Flow, choose `OpenSearchIndexDocument`, enter a unique Flow ID, and use a document ID reserved for this Flow:

```json
{"index":"products","documentId":"dex-example-sku-42","source":{"title":"Meridian","category":"release"}}
```

The pull Worker connects to Dex Server without an inbound listener. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/document-index/flow` while `dexcli dev` runs.
