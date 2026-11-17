# EasyAPI E-Invoice Python connector

`dex-connector-easyapi` implements the read-only `queryShop` Query for Dex Flows. It follows the [official company API](https://fapiao.easyapi.com/docs/shop/) and [authentication contract](https://fapiao.easyapi.com/docs/auth/): HTTPS `GET /shop/query` with service `appKey`, `appSecret`, and authorized company `taxNumber` in the query string. The connector disables redirects and returns generic errors without URL or provider response text.

## Connection

In Dex Web, create the static `easyapi` connection. Sign in to the [EasyAPI E-Invoice console](https://fapiao.easyapi.com/console/), open **服务认证**, and create or select the service key pair associated with the invoicing company. Copy the Key into protected `app_key` and paired Secret into protected `app_secret`; both are required and blank prevents requests. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for displayed defaults. The base URL is an HTTPS origin; Python timeout is measured in seconds and response limit in bytes.

Start the Query with that company's 15, 17, 18, 19, or 20-character uppercase alphanumeric tax number. The key pair must be associated with this company. Only `shopId`, company `name`, `apiState` (1 normal, 0 expired, -1 not activated), and `endTime` are returned. The result excludes tax number, key, secret, address, phone, banking, staff, certificate, and other provider fields. HTTP 429/5xx and transport failures retry; definite provider errors select optional `providerRejected`, malformed or oversized responses select `invalidResponse`, and invalid local inputs select `defect`.

The [runnable shop Flow](examples/shop/README.md) stores only the selected service status in a Dex Attribute. Its integration test uses inert credentials and a fake provider with a real Dex Worker and Client. Live EasyAPI authorization and production company status remain unverified without customer-controlled credentials.
