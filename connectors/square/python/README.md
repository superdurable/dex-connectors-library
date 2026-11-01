# Square connector for Python

Install with `pip install dex-connector-square==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `listPayments` and `getPayment` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listPayments` | `new_list_payments_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getPayment` | `new_get_payment_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`listPayments` passes the optional `cursor` from the previous page.
`getPayment` requires one path segment and unwraps Square's `payment` object.
HTTP 404 selects `notFound`; 408, 429, 5xx, and transport failures retry; 401,
403, and other non-200 responses, including 409, select `providerRejected`.
Every request sends `Square-Version: 2026-09-16`.

## Authorization

Create a Dex Web connection named `square` as described in the
[connector README](../README.md#authentication-and-configuration). The access
token travels only in the `Authorization: Bearer` header and is resolved for
every attempt.

## Example

[`examples/resource_read`](examples/resource_read/flow.py) reads one page of
payments and records it. Run the Worker with
`uv run python -m examples.resource_read.worker` after `dexcli dev` has loaded
a project containing the `square` connection.

## Tests

`squaremock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
