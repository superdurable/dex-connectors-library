# Elasticsearch document delete Flow

This runnable Flow calls `deleteDocument` with the static `elasticsearch` connection and completes once the target ID is absent. A document already absent is also a successful result. Optional branches are unwired, so a missing index or provider rejection fails the Flow.

## Strict FDG 2.0 generation

From `connectors/elastic/elasticsearch/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/document-delete/flow/workflow.go --schema-version 2.0 --json --out ./build/document-delete
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override elasticsearch=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `elasticsearch / elasticsearch`. Use a dedicated native user with `delete` permission for the target index; see [Elasticsearch role permissions](https://www.elastic.co/docs/deploy-manage/users-roles/cluster-or-deployment-auth/kibana-role-management). Set the HTTPS cluster REST endpoint, username, and secret password. Leave the safety limits blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/elastic/elasticsearch/go`:

```bash
go run ./examples/document-delete
```

In Dex Web Start Flow, choose `ElasticsearchDeleteDocument`, enter a unique Flow ID, and use a document ID owned by this example:

```json
{"index":"products","documentId":"dex-example-sku-42"}
```

The pull Worker connects to Dex Server without an inbound listener. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/document-delete/flow` while `dexcli dev` runs.
