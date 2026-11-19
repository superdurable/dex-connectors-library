# Dada order status for Dex TypeScript

`@superdurable/connector-dada` reads one Dada delivery order's current status. The `queryOrderStatus` Query accepts `{ orderID }` containing the third-party `order_id` used when creating the delivery order. It returns only `orderID`, `statusCode`, and `statusMessage`; rider identity and phone fields are excluded. `found` is the happy branch. `providerRejected`, `invalidResponse`, and `defect` are optional terminal branches. Transport, 429, and 5xx failures return Retry.

## Connection

Create a Dex Web Connection named `dada`. At the [Dada Open Platform](https://newopen.imdada.cn), open **开发助手 → 应用信息** for `app_key` and its secret `app_secret`; open **账户中心 → 基本资料** for the merchant `source_id`. Put them in `appKey`, secret `appSecret`, and `sourceID`. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. The endpoint is POST `/api/order/status/query`; the request uses the [official uppercase MD5 signing algorithm](https://newopen.imdada.cn/#/development/file/algorithm).

## Runnable Flow

The [order status example](examples/order-status/README.md) includes a Worker and Client and persists the status. Install with `npm install`, run `npm run example:worker`, then `npm run example:start -- 20170301000001` after configuring Dex.

Provider conformance replays 13 Go-recorded scenarios and the official signature vector. The integration test runs the same Flow through a real Dex Worker against a local provider fixture. Real Dada authorization and order lookup remain unverified without a user-owned connection.
