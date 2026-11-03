# Pinecone TypeScript connector

`@superdurable/connector-pinecone` supplies `upsertVectors` and `queryVectors`
for Dex TypeScript Flows. The typed operation models, Step factories, connection
contracts, and mocks are generated from [`connector.yaml`](../connector.yaml).
The [runnable sample](examples/sample-pinecone/) binds the static `pinecone`
connection and writes one batch through `newUpsertVectorsStep`.

## Install and connect

```sh
npm install @superdurable/connector-pinecone @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. In Dex Web
Connections, choose Pinecone, enter the index's HTTPS host as `baseUrl`, and
store the project API key as the private `api_key`. Start at
[app.pinecone.io](https://app.pinecone.io), open your project, select **Indexes**
for the host and **API keys** for the key. A project `timeout` override accepts a positive Go duration such as `45s`; it is applied to each provider request. Leave `timeout` and
`maxResponseBytes` blank for the manifest defaults. Credentials resolve for
every operation; never put a key in Flow input, Attributes, Results, or logs.

The sample Worker calls `loadFromEnvironment()` and `newProjectConnection()`
with `ConnectionName`. See its runnable source for the complete Flow wiring.

## Operations and failures

| Factory | Pinecone endpoint | Happy branch |
| --- | --- | --- |
| `newUpsertVectorsStep` | `POST /vectors/upsert` | `upserted` |
| `newQueryVectorsStep` | `POST /query` | `queried` |

`upsertVectors` replaces stable vector IDs and is safe to retry with the same
input. The connector derives its stable key from the Dex Call ID; Pinecone does
not receive an idempotency header. `queryVectors` searches dense vectors in one
namespace. Both operations use API version `2025-04`.
Vector coordinates are encoded at float32 precision, matching the Go connector;
nonfinite or overflowing coordinates are rejected before a request. Query
results preserve provider JSON number text, including large numeric metadata.

A definite provider rejection selects the optional `providerRejected` branch.
Malformed, incomplete, or oversized success bodies select `invalidResponse`.
Invalid local input or credentials select `defect`. HTTP 408, 409, 429, 5xx,
and transport failures request Dex retry. An unwired optional branch fails the
Flow. Provider error bodies are never included in Failures.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run
`npm run test:integration`. The test executes the checked-in sample through a
real Dex Worker and Client against a temporary local HTTP fixture. The shared
Go recording replays both operations' requests, outputs, branches, retries,
and mutation key. Real Pinecone authorization and live provider calls remain
unverified.
