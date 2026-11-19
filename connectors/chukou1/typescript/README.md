# Chukou1 connector for Dex TypeScript

`@superdurable/connector-chukou1` implements the read-only `listWarehouses` Query. It sends `GET /v1/warehouses` with the connected Bearer token and maps the [official response array](https://openapi.chukou1.cn/v1/Help/Api/GET-v1-warehouses) to `{warehouses: [{id, name, country, countryCode}]}`. It lists at most 200 records and returns no order, inventory, address, or customer data.

Create a Dex Web connection named `chukou1`. For a self-developed integration, sign in at <https://yewu.chukou1.cn/Client/Index.aspx>, open **Account > Customer Authorization Management > Authorize > View Token**, and paste the token without `Bearer` into the protected `access_token` field. The [authorization guide](https://openapi.chukou1.cn/v1/Help/SelfAuthorization) describes the fixed token; third-party ERP applications use a separate OAuth2 approval and consent process. Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their production defaults unless the account has an approved override.

`listed` returns the warehouse page, including an empty array. A definite rejection selects the optional `providerRejected` branch; malformed or oversized data selects `invalidResponse`; an invalid or missing token selects `defect` before any request. HTTP 408, 429, 5xx, and transport failures Retry through Dex. Unwired optional branches fail the Flow.

[Run the warehouse list Flow](examples/warehouse-list/README.md). Its real Dex integration test exercises a provider 503 followed by success and verifies persistence. Twelve Go-recorded [conformance scenarios](../../../conformance/connectors/chukou1/scenarios.json) check exact requests and outcomes. Production Chukou1 authorization and warehouse responses have not been verified.
