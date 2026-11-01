# Contentful connector for Python

Install with `pip install dex-connector-contentful==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getEntry` and `listEntries` with the same
branches, defaults, and failure classification as the Go module. Each output
keeps the provider's JSON response untouched in `data`.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getEntry` | `new_get_entry_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listEntries` | `new_list_entries_step` | `listed` | `providerRejected`, `invalidResponse`, `defect` |

Space, environment, and entry IDs must be letters, digits, `_`, or `-`; `listEntries` accepts `limit` up to 1000 and a nonnegative `skip`. 408, 409, 429, 5xx, and transport failures retry. HTTP 404
selects `notFound` where the operation declares it; other non-200 responses
select `providerRejected` with authentication (401), authorization (403), or
validation (400, 422) failure kinds. A successful body without the expected
top-level shape is `invalidResponse`.

## Authorization

Create a Dex Web connection named `contentful` as described in the
[connector README](../README.md#authorization-and-configuration). The Content Delivery API token travels only in the `Authorization: Bearer` header and is resolved for every attempt.

## Example

[`examples/sample_contentful`](examples/sample_contentful/flow.py) runs one
`getEntry` read and records the result. Run the Worker with
`uv run python -m examples.sample_contentful.worker` after `dexcli dev` has
loaded a project containing the `contentful` connection.

## Tests

`contentfulmock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
