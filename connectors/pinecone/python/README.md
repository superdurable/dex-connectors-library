# Pinecone connector for Python

Install with `pip install dex-connector-pinecone==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `upsertVectors` (Mutation) and `queryVectors` (Query) with
the same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `upsertVectors` | `new_upsert_vectors_step` | `upserted` | `providerRejected`, `invalidResponse`, `defect` |
| `queryVectors` | `new_query_vectors_step` | `queried` | `providerRejected`, `invalidResponse`, `defect` |

`upsertVectors` posts 1 through 1000 vectors with unique stable IDs to
`/vectors/upsert`; replacing by ID makes a retry safe. Coordinates are rounded
to 32-bit floats and written exactly as the Go module writes them. Only an
`upsertedCount` from zero through the batch size selects `upserted`.
`queryVectors` posts one dense vector and a `topK` from 1 through 10000 to
`/query` and returns Pinecone's JSON, which must contain a `matches` list,
untouched in `data`.

Both operations retry on 408, 409, 429, 5xx, and transport failures; other
non-200 statuses select `providerRejected` with authentication (401),
authorization (403), or validation (400, 422) kinds. Every request sends
`X-Pinecone-Api-Version: 2025-04`.

## Authorization

Create a Dex Web connection named `pinecone` as described in the
[connector README](../README.md#authorization-and-configuration), with the
index host as `baseUrl`. The API key travels only in the `Api-Key` header and
is resolved for every attempt.

## Example

[`examples/sample_pinecone`](examples/sample_pinecone/flow.py) upserts the start
input's vectors. Run the Worker with
`uv run python -m examples.sample_pinecone.worker` after `dexcli dev` has
loaded a project containing the `pinecone` connection.

## Tests

`pineconemock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
