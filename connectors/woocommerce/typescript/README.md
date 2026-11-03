# WooCommerce TypeScript connector

Install `@superdurable/connector-woocommerce` and `@superdurable/dex`. The package provides `newListOrdersStep` and `newGetOrderStep` with the same operation IDs and branches as the Go connector. `listOrders` returns a page of order summaries; `getOrder` returns one order. The `listed` and `found` branches are the happy paths. Unwired `notFound`, `providerRejected`, `invalidResponse`, and `defect` branches fail the Flow.

The [resource read example](examples/resource-read/README.md) uses a static `woocommerce` connection. Its real Dex integration test runs the same Flow with a local provider fixture.

Configure `storeUrl` with the HTTPS WordPress Site Address from Settings > General, including any site path and excluding `/wp-json/wc/v3`; the connector appends that API path. `timeout` is a positive Go duration for one request, and `maxResponseBytes` is a positive byte limit for a successful JSON response. Blank optional values use the displayed defaults. In WooCommerce > Settings > Advanced > REST API, create a Read permission key. Store `consumer_key` and `consumer_secret` as Dex credentials. The connector resolves both on every call and sends them only in the Basic Authorization header.

A 404 routes to `notFound`; 401 and 403 route to `providerRejected` with their respective authentication and authorization failure kinds. Other definite 4xx statuses also route to `providerRejected`. A 408, 429, 5xx, or transport failure retries. Malformed or oversized successful JSON routes to `invalidResponse`; invalid input or unavailable credentials routes to `defect`. The provider error body is never copied into a failure.

Live WooCommerce authorization and provider responses have not been verified with a user-controlled connection.
