# Chuanglan 253 connector for Dex TypeScript

`@superdurable/connector-chuanglan` implements the read-only `getBalance` Query. It sends `POST https://smssh1.253.com/msg/balance/json` with the connected domestic SMS API account and password in a JSON request body, following the [official balance API](https://doc.chuanglan.com/docs/4XVHFKRMBD6M4T9C). It returns only `{remainingMessages}`; credentials and provider error text are excluded from Flow state, results, receipts, and logs.

Create a Dex Web connection named `chuanglan`. Open <https://zz.253.com>, then **产品概览 > API 接口信息** and copy the domestic SMS API account and API password into the two secret connection fields. Set `baseUrl` to the HTTPS origin assigned to that account when it differs from the East China default. The connection guide also covers outbound IP allowlisting. `timeout` and `maxResponseBytes` have safe defaults and can be left blank.

A successful provider `code` of numeric or string `0` selects `fetched` with a nonnegative count. A definite provider rejection selects the optional `providerRejected` branch. Malformed or oversized responses select `invalidResponse`; missing credentials select `defect` without a request. HTTP 408, 429, 5xx, and transport errors Retry through Dex. Unwired optional branches fail the Flow.

[Run the balance Flow](examples/balance/README.md). Its real Dex integration test exercises a provider 503 followed by success and verifies persistence. Eleven Go-recorded [conformance scenarios](../../../conformance/connectors/chuanglan/scenarios.json) check exact requests and outcomes. A real Chuanglan account and live balance lookup have not been verified.
