# WooCommerce connector for Python

Install with `pip install dex-connector-woocommerce==0.1.0`. The package
requires `dex-connectors-sdk==0.1.0` and `dex-python-sdk>=1.28.0`, and
implements the manifest Queries `listOrders` and `getOrder` with the same
branches, defaults, and failure classification as the Go module.

## Operations

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listOrders` | `new_list_orders_step` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getOrder` | `new_get_order_step` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Requests go to `<storeUrl>/wp-json/wc/v3`. `listOrders` passes optional `page`
and `per_page` (at most 100); `getOrder` requires a positive order ID. HTTP 404
selects `notFound`; 408, 429, 5xx, and transport failures retry; 401, 403, and
other non-200 responses select `providerRejected`.

## Authorization

Create a Dex Web connection named `woocommerce` as described in the
[connector README](../README.md#authentication-and-configuration). The
consumer key and secret travel only as HTTP Basic credentials and are resolved
for every attempt.

## Example

[`examples/resource_read`](examples/resource_read/flow.py) reads one page of
orders and records it. Run the Worker with
`uv run python -m examples.resource_read.worker` after `dexcli dev` has loaded
a project containing the `woocommerce` connection.

## Tests

`woocommercemock.Mock` scripts both Queries and defaults to the manifest
answers. Run `uv run pytest tests` for provider parity and manifest contract
checks.
