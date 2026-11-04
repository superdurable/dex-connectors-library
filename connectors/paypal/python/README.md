# PayPal connector for Python

Install with `pip install dex-connector-paypal==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getOrder` and `listTransactions` with the same branches,
defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getOrder` | `new_get_order_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listTransactions` | `new_list_transactions_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`getOrder` reads `/v2/checkout/orders/<orderId>` and returns the order ID and
status. `listTransactions` reads one page of `/v1/reporting/transactions` for
an RFC 3339 window of at most 31 days and returns each transaction's ID,
status, and amount.

Each client exchanges the REST app's client ID and secret at
`/v1/oauth2/token` and caches the bearer token, per credential pair, until 30
seconds before it expires (at most 24 hours). A 401 from a read clears the
cache. Reads retry on 408, 429, 5xx, and transport failures; 404 selects
`notFound`; other statuses select `providerRejected`. A rejected token
exchange selects `providerRejected`, and a malformed token response selects
`invalidResponse`.

## Authorization

Create a Dex Web connection named `paypal` as described in the
[connector README](../README.md#authentication-and-configuration). The client
secret travels only in the token exchange's Basic `Authorization` header and
is resolved for every attempt.

## Example

[`examples/resource_read`](examples/resource_read/flow.py) reads one checkout
order. Run the Worker with `uv run python -m examples.resource_read.worker`
after `dexcli dev` has loaded a project containing the `paypal` connection.

## Tests

`paypalmock.Mock` scripts both operations and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
