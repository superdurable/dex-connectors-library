# Elasticsearch document search Flow

This runnable Flow calls `searchDocuments` with the static `elasticsearch` connection and completes with the search page. Optional branches are unwired, so a missing index or provider rejection fails the Flow.

## Strict FDG 2.0 generation

From `connectors/elastic/elasticsearch/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/document-search/flow/workflow.go --schema-version 2.0 --json --out ./build/document-search
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override elasticsearch=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `elasticsearch / elasticsearch`. Find the cluster HTTPS REST endpoint in your deployment console. In Kibana, create a native user under **Stack Management > Security > Users** and grant the index pattern's `read` permission through **Stack Management > Security > Roles**. Save the username and password in the connection; leave timeout and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/elastic/elasticsearch/go`:

```bash
go run ./examples/document-search
```

In Dex Web Start Flow, choose `ElasticsearchDocumentSearch`, enter a unique Flow ID, and use:

```json
{"index":"products","query":"title:Meridian","size":10,"from":0}
```

Replace the index and query with values permitted to the connection's user. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show the persisted search page. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/document-search/flow` while `dexcli dev` runs.
