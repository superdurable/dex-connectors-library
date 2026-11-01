# Webflow connector for Python

Install with `pip install dex-connector-webflow==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listCollectionItems` and `getCollectionItem` with the same
branches, defaults, and failure classification as the Go module. Each output
keeps the provider's JSON response untouched in `data`.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listCollectionItems` | `new_list_collection_items_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `getCollectionItem` | `new_get_collection_item_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Collection and item IDs must be 24 hexadecimal characters; `listCollectionItems` accepts `limit` up to 100 and a nonnegative `offset`. 408, 409, 429, 5xx, and transport failures retry. HTTP 404
selects `notFound` where the operation declares it; other non-200 responses
select `providerRejected` with authentication (401), authorization (403), or
validation (400, 422) failure kinds. A successful body without the expected
top-level shape is `invalidResponse`.

## Authorization

Create a Dex Web connection named `webflow` as described in the
[connector README](../README.md#authorization-and-configuration). The site API token travels only in the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/sample_webflow`](examples/sample_webflow/flow.py) runs one
`listCollectionItems` read and records the result. Run the Worker with
`uv run python -m examples.sample_webflow.worker` after `dexcli dev` has
loaded a project containing the `webflow` connection.

## Tests

`webflowmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
