# Klaviyo connector for Python

Install with `pip install dex-connector-klaviyo==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest operations `getProfile` (Query) and `createEvent` (Mutation) with the
same branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getProfile` | `new_get_profile_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createEvent` | `new_create_event_step` | `accepted` | `providerRejected`, `defect` |

`createEvent` sends the SHA-256 hex digest of the stable call ID as the
event's `unique_id`, so Klaviyo deduplicates retries; only HTTP 202 selects
`accepted`. Both operations retry on 408, 409, 429, 5xx, and transport
failures; 401 and 403 select `providerRejected` with authentication and
authorization kinds. Every request sends `revision: 2026-10-15`.

## Authorization

Create a Dex Web connection named `klaviyo` as described in the
[connector README](../README.md#authorization). The private API key travels
only in the `Authorization: Klaviyo-API-Key` header and is resolved for every
attempt.

## Example

[`examples/profile_event`](examples/profile_event/flow.py) submits one event
for a profile email. Run the Worker with
`uv run python -m examples.profile_event.worker` after `dexcli dev` has loaded
a project containing the `klaviyo` connection.

## Tests

`klaviyomock.Mock` scripts both operations and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
