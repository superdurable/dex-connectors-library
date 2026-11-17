# Dada Express Python connector

`dex-connector-dada` implements the read-only `queryOrderStatus` Query for Dex Flows. It POSTs to the [official order status endpoint](https://newopen.imdada.cn/#/development/file/statusQuery) and returns the order ID, status code, and status label. Rider names, phone numbers, and location are excluded from the output.

## Connection

Create a static `dada` connection in Dex Web. Sign in to the [Dada Open Platform](https://newopen.imdada.cn), open **开发助手 > 应用信息**, and copy `app_key` into `appKey` and paired secret `app_secret` into the secret `appSecret` field. Find the merchant `source_id` under **账户中心 > 基本资料** and enter its full decimal identifier in `sourceID`. All three are required; blank prevents calls. The application must have access to that merchant's orders.

Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their displayed defaults. `baseUrl` is the HTTPS origin without a path; override only for a trusted compatible gateway. `timeout` is measured in seconds in Python; `maxResponseBytes` is measured in bytes. The Query input `orderID` is the third-party order ID supplied when creating the delivery order, up to 128 UTF-8 bytes.

The connector signs each request according to the [Dada signing rules](https://newopen.imdada.cn/#/development/file/algorithm): uppercase MD5 of `app_secret` + sorted key/value pairs + `app_secret`. It never sends the secret itself. HTTP 429, 5xx, and transport errors retry. Definite provider rejections select optional `providerRejected`; malformed or oversized responses select `invalidResponse`; invalid local input or credentials select `defect`.

The [runnable order status Flow](examples/order_status/README.md) stores only non-private status fields in a Dex Attribute. Its integration test runs a real Dex Worker and Client against an inert fake provider. Live merchant authorization and production order status remain unverified without user-controlled credentials and an existing delivery order.
