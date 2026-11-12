# Algolia connector for Python

Install with `pip install dex-connector-algolia==0.2.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `search` (Query) and `saveObject` (Mutation) with the same
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `search` | `new_search_step` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `saveObject` | `new_save_object_step` | `saved` | `providerRejected`, `invalidResponse`, `defect` |

`search` returns Algolia's hit count and each hit's `objectID` and optional
`name`; `hitsPerPage` ranges from 0 (Algolia's default) through 1000.
`saveObject` replaces the whole record at its stable `objectID`, so a retry
writes the same absolute value; only a response echoing the `objectID` with a
positive `taskID` selects `saved`. Indexing completes asynchronously.

Both operations retry on 408, 409, 429, 5xx, and transport failures; 401 and
403 select `providerRejected` with authentication and authorization kinds.
With the default base URL, each call targets `https://<application ID>.algolia.net/1`.

## Authorization

Create a Dex Web connection named `algolia` as described in the
[connector README](../README.md#authorization). The API key travels only in
the `x-algolia-api-key` header and is resolved for every attempt.

## Example

[`examples/index_search`](examples/index_search/flow.py) searches one index
for up to ten hits. Run the Worker with
`uv run python -m examples.index_search.worker` after `dexcli dev` has loaded
a project containing the `algolia` connection.

## Tests

`algoliamock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
