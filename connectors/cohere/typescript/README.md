# Cohere TypeScript connector

Install `@superdurable/connector-cohere` to call Cohere v2 `embed` and `rerank`
from a Dex TypeScript Flow. Both operations are Query Steps. The package binds a
named connection, resolves the secret API key for each attempt, and keeps it out
of Flow state and failure messages.

Create a `cohere` connection named `cohere` in Dex Web Connections. Create an API
key at [Cohere Dashboard > API Keys](https://dashboard.cohere.com/api-keys) and
save it in secret `api_key`. The manifest supplies the API URL, timeout, and
response limit defaults.

The [Cohere search example](examples/cohere-search/README.md) is the runnable
consumer. It embeds the search query, reranks the documents, and completes with
ranked indices. Optional failure branches are unwired and fail the Flow.
HTTP 408, 409, 429, 5xx, and transport errors Retry with the manifest's billed
operation budget. Local validation or credential errors select `defect`;
definitive provider rejections select `providerRejected`; malformed or oversized
successful responses select `invalidResponse`.

From the repository root, regenerate and verify the manifest contract with:

```sh
GOWORK=off go run ./cmd/connectorctl generate connectors/cohere/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/cohere/connector.yaml
make test-connector-conformance CONNECTOR_DIRECTORY=connectors/cohere
```

The conformance fixture replays Go-recorded HTTP requests and results. Live
Cohere authorization and responses have not been verified.
