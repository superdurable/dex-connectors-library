# Shopify connector for TypeScript

Install `@superdurable/connector-shopify@0.1.0`. This package provides the
`newGetOrderStep` and `newListOrdersStep` Query factories, typed models, a named
connection, and manifest-generated mocks. The happy branches are `found` and
`listed`; `notFound`, `providerRejected`, `invalidResponse`, and `defect` are
optional and fail an unwired Flow.

The client reads `https://<shopSubdomain>.myshopify.com/admin/api/2026-10`.
`getOrder` requires a positive numeric order ID. `listOrders` requests
`status=any` and accepts a limit from 1 through 250; zero uses Shopify's
default. HTTP 408, 429, 5xx, and transport failures retry. Credentials are
resolved for each attempt and sent only in `X-Shopify-Access-Token`.

Create a private Dex Web connection named `shopify-orders` with the fields in
the [manifest authorization guide](../README.md#authentication-and-configuration).
The `timeout` setting is a positive Go duration; the TypeScript client converts
it to milliseconds. Blank `timeout` and `maxResponseBytes` use manifest defaults.

The [runnable order read example](examples/order-read/README.md) is the
consuming application. Run `npm ci && npm run check` in this directory for
the package and Go-recorded provider conformance tests. Run the example's
integration test against `dexcli dev` to verify a real Worker, Client, and
unwired rejection. Live Shopify authorization and provider behavior have not
been verified.
