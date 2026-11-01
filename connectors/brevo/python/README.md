# Brevo connector for Python

Install with `pip install dex-connector-brevo==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `createOrUpdateContact` (Mutation) and `getContact`
(Query) with the same branches, defaults, and failure classification as the Go
module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `createOrUpdateContact` | `new_create_or_update_contact_step` | `saved` | `providerRejected`, `invalidResponse`, `defect` |
| `getContact` | `new_get_contact_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Both take a plain email address without a display name. The upsert sends
`updateEnabled: true`, so repeating it is safe; attribute names must be
uppercase, such as `FNAME`. HTTP 201 returns the new contact ID and 204 means
an existing contact was updated. 408, 409, 429, 5xx, and transport failures
retry; 401 and 403 select `providerRejected` with authentication and
authorization kinds.

## Authorization

Create a Dex Web connection named `brevo` as described in the
[connector README](../README.md#authorization). The API key travels only in the
`api-key` header and is resolved for every attempt.

## Example

[`examples/contact_sync`](examples/contact_sync/flow.py) creates or updates one
contact with a first name. Run the Worker with
`uv run python -m examples.contact_sync.worker` after `dexcli dev` has loaded a
project containing the `brevo` connection.

## Tests

`brevomock.Mock` scripts both operations and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
