# Chuanglan 253 Python connector

`dex-connector-chuanglan` v0.1.0 implements the read-only `getBalance` Query.
It sends the connected domestic SMS API account and password in a JSON POST to
the account's HTTPS node at `/msg/balance/json`, as described in the
[official balance API](https://doc.chuanglan.com/docs/4XVHFKRMBD6M4T9C).
Credentials are resolved on each attempt and never appear in Flow input,
results, receipts, or error messages. A successful lookup returns only
`remaining_messages`, the number of SMS messages left.

The Query selects `fetched`, `providerRejected`, `invalidResponse`, or
`defect`. HTTP 429, 408, 5xx, and transport failures retry because the
operation is read-only. A response over `maxResponseBytes` selects
`invalidResponse`. The example wires only `fetched`; optional branches fail
its Flow.

Use the [runnable balance Flow](examples/balance/README.md) with the static
`chuanglan` connection. Its Dex Web setup shows the authorization fields
`account` and `password`, plus `baseUrl`, `timeout`, and `maxResponseBytes`.
There is no operation-specific configuration UI. The provider console
[产品概览 > API 接口信息](https://zz.253.com) supplies the API credentials and
account node. The API account is distinct from the console login. Leave
non-secret configuration fields blank to use manifest defaults.

Go-recorded conformance scenarios cover every branch, 429 and 5xx Retry,
transport failure, response size, and missing credentials. The checked-in
example also runs against a real Dex Worker with a fake provider. Live
Chuanglan authorization and the console setup page remain unverified.
