# Dada Express connector

The Go `queryOrderStatus` Query reads the current delivery state of a third-party order through the [official order detail API](https://newopen.imdada.cn/#/development/file/statusQuery). Supply the same `orderID` that was sent as `order_id` when the Dada delivery order was created. The result contains only the order ID, status code, and status label; rider names, phone numbers, and location are omitted. The Query does not create, cancel, or modify an order.

## Connect

At the [Dada Open Platform](https://newopen.imdada.cn), sign in as a developer, open **开发助手 → 应用信息**, and copy `app_key` to `appKey` and secret `app_secret` to `appSecret` in Dex Web Connections. Find the merchant `source_id` under **账户中心 → 基本资料** and enter it in `sourceID`. The application must be authorized to query that merchant's orders. These are the three required credential fields; leave the transport settings blank for their displayed defaults.

The [API request rules](https://newopen.imdada.cn/#/development/file/api) define a JSON POST to the production HTTPS domain, and the [signature guide](https://newopen.imdada.cn/#/development/file/algorithm) defines uppercase MD5 of `app_secret` + sorted key/value pairs + `app_secret`. The connector signs each request and never sends `app_secret` itself. HTTP 429, HTTP 5xx, and transport failures retry. Definite provider rejections select optional `providerRejected`; malformed or oversized responses select optional `invalidResponse`. The example leaves optional branches unwired, so those outcomes fail the Flow. The public documentation does not state a fee for this status Query; check current pricing in the portal.

Runnable examples are in [`go/examples/order-status`](go/examples/order-status), [`python/examples/order_status`](python/examples/order_status), and [`typescript/examples/order-status`](typescript/examples/order-status). Provider tests verify the official signature vector and HTTP request against a local server. Live merchant authorization and production order status remain unverified without user-controlled credentials and an existing delivery order.

## Python

The [Python package](python/README.md) implements `queryOrderStatus` and includes a [runnable Dex Flow](python/examples/order_status/README.md). Its 13 provider scenarios replay Go-recorded signed requests, outputs, and retry decisions. No live merchant account has been authorized for this port.

## TypeScript

The [TypeScript package](typescript/README.md) implements the same signed query and includes a runnable Dex Flow. Its 13 provider scenarios replay Go-recorded requests and retry decisions.
