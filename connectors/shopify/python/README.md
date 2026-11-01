# Shopify connector for Python

Install with `pip install dex-connector-shopify==0.1.0`. The package requires
`dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and implements the
manifest Queries `getOrder` and `listOrders` with the same branches, defaults,
and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `getOrder` | `new_get_order_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listOrders` | `new_list_orders_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Requests go to `https://<shopSubdomain>.myshopify.com/admin/api/2026-10`.
`getOrder` requires a positive order ID and unwraps Shopify's `order` object;
`listOrders` reads orders in any status with an optional `limit` of at most
250. HTTP 404 selects `notFound`; 408, 429, 5xx, and transport failures retry;
401, 403, and other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `shopify` as described in the
[connector README](../README.md#authentication-and-configuration). The Admin
API access token travels only in `X-Shopify-Access-Token` and is resolved for
every attempt.

## Example

[`examples/resource_read`](examples/resource_read/flow.py) reads one order and
records it. Run the Worker with
`uv run python -m examples.resource_read.worker` after `dexcli dev` has loaded
a project containing the `shopify` connection.

## Tests

`shopifymock.Mock` scripts both Queries and defaults to the manifest answers.
Run `uv run pytest tests` for provider parity and manifest contract checks.
