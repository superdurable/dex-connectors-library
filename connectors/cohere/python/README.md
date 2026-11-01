# Cohere connector for Python

Install with `pip install dex-connector-cohere==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `embed` and `rerank` with the same branches, defaults, and
failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `embed` | `new_embed_step` | `embedded` | `providerRejected`, `invalidResponse`, `defect` |
| `rerank` | `new_rerank_step` | `ranked` | `providerRejected`, `invalidResponse`, `defect` |

`embed` takes a model, 1 to 96 nonblank texts, and an input type
(`search_document`, `search_query`, `classification`, or `clustering`) and
returns one float vector per text. `rerank` takes a model, a query, 1 to 1000
nonblank documents, and an optional `top_n`, and returns each document's index
and relevance score in ranked order. Both calls may be billed, so the manifest
allows three attempts within five minutes. 408, 409, 429, 5xx, and transport
failures retry; 401 and 498 select `providerRejected` with an authentication
kind, and 403 with an authorization kind.

## Authorization

Create a Dex Web connection named `cohere` as described in the
[connector README](../README.md#connection). The API key travels only in the
`Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/cohere_search`](examples/cohere_search/flow.py) embeds a query and
then reranks candidate documents. Run the Worker with
`uv run python -m examples.cohere_search.worker` after `dexcli dev` has loaded
a project containing the `cohere` connection.

## Tests

`coheremock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
