# Square connector for TypeScript

Install `@superdurable/connector-square@0.1.0`. It provides `listPayments` and
`getPayment` as Dex Query operations with manifest-generated Step factories.

| Operation | Factory | Happy branch | Optional branches |
| --- | --- | --- | --- |
| `listPayments` | `newListPaymentsStep` | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getPayment` | `newGetPaymentStep` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

Use the [runnable payment Flow](examples/resource-read/flow/workflow.ts) with the
static connection name `square`. `newProjectConnection` reads `baseUrl`,
`timeout`, and `maxResponseBytes` from the Dex Web connection and resolves its
private `access_token` for each request. The token is sent only in a bearer
header. Every request sends `Square-Version: 2026-09-16`.

HTTP 404 selects `notFound`; 401, 403, and other definite rejections select
`providerRejected`. HTTP 408, 429, 5xx, and transport failures retry. Malformed
or oversized success JSON selects `invalidResponse`. Invalid local input or
unavailable credentials select `defect`.

Run `npm run check` for package tests and `npm run test:integration` with a real
`dexcli dev` service. Provider replay is checked against Go recordings in
`conformance/connectors/square/scenarios.json`. A real Square account and live
provider authorization were not available for this port.
