# Cohere search example

This Worker registers `CohereSearch` with the static `cohere` connection. A typed Start Flow request supplies a query and at least two documents. The Flow persists the request, calls `embed` with `input_type: search_query` and `embedding_types: ["float"]`, then calls `rerank` for the original documents and completes with ranked indices and scores.

## Generate and run

From `connectors/cohere`, generate strict FDG 2.0 from the checked-in Flow source:

```bash
mkdir -p /tmp/cohere-search-fdg
dexcli visualize ./examples/cohere-search/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/cohere-search-fdg
```

The command must report `valid: true`. A local module may also report `connector_release_required` until the first release. From the repository root, create local release metadata for Dex Web Connections:

```bash
mkdir -p /tmp/cohere-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/cohere/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/cohere/go \
  --version v0.2.0 --tag connectors/cohere/go/v0.2.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/cohere-release/connector-release.json \
  --digest-output /tmp/cohere-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp/cohere-search-fdg \
  --connector-release-override cohere=/tmp/cohere-release
```

Configure the static `cohere` connection in Dex Web, then in a second terminal run `GOWORK=off go run ./examples/cohere-search` from `connectors/cohere` with the project environment described in [`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md). Start `CohereSearch` through Dex Web's Start Flow form.

The deterministic mapping unit test runs with `GOWORK=off go test ./examples/cohere-search/...`. CI runs `flow/workflow_mock_integration_test.go` against a real Dex Worker and Client; local integration is left to CI.
