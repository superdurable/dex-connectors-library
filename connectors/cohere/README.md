# Cohere connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/cohere/go@v0.3.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/cohere`.

`github.com/superdurable/dex-connectors-library/connectors/cohere/go` exposes `chat`, `embed`, and `rerank` as Dex Query Steps using Cohere's public v2 JSON API. Python and TypeScript currently expose `embed` and `rerank`. Calls use a connection-owned secret API key; the key stays out of URLs, Flow state, results, failures, and receipts.

## Connection

In Dex Web Connections, create a `cohere` connection named `cohere`. Open [Cohere Dashboard > API Keys](https://dashboard.cohere.com/api-keys), create a key, and paste its opaque value into the secret `api_key` field. Leave baseUrl, timeout, and maxResponseBytes blank for their displayed defaults.

The Python package is [`dex-connector-cohere`](python/README.md), imported as
`dex_connectors.cohere`.

The TypeScript package is [`@superdurable/connector-cohere`](typescript/README.md).
Its runnable search Flow uses the static `cohere` connection, calls both
operations, and verifies retry and rejection behavior against a local provider
stand-in through a real Dex Worker and Client. Live Cohere authorization and
responses remain unverified.

## Operations

- [`chat`](https://docs.cohere.com/v2/reference/chat) sends one to 100 chronological text messages to `POST /v2/chat`. It returns the assistant's concatenated text, response ID, and finish reason. Tool calls and non-text replies select `invalidResponse`; this operation supports text generation only.
- [`embed`](https://docs.cohere.com/reference/embed) sends up to 96 nonempty text inputs to `POST /v2/embed`, with a required model and input type. It fixes `embedding_types` to `float` and returns one float vector per input.
- [`rerank`](https://docs.cohere.com/reference/rerank) sends a model, query, and up to 1,000 documents to `POST /v2/rerank`, returning indices and relevance scores in ranked order. `topN` optionally limits the returned count.

These operations select a success branch on HTTP 200, `providerRejected` on definitive 4xx, `invalidResponse` on malformed or oversized JSON, and `defect` on invalid local input or credentials. HTTP 408, 409, 429, 5xx, and transport failure Retry for up to three attempts. An ambiguous attempt may incur another billed request. Provider error-body text is never placed in a failure. The Cohere v2 inference endpoints do not create provider resources, so no resource write operation applies.

## Mocks

The generated `coheremock` package's `embed` default returns one three-dimensional float vector for a Meridian query; its `rerank` default ranks a release note above an unrelated article. Both operations include an invalid-key rejection mock.

## Example

The runnable [search example](go/examples/cohere-search/README.md) registers a Worker and a Flow that calls all three Go operations. Its mock-backed real-Dex integration test verifies the provider call sequence.

## Live verification

The API endpoint, authentication, fields, and statuses above were checked against Cohere's linked official docs. Provider authorization and live responses have not been exercised.
