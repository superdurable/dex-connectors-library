# Chukou1 overseas warehouse catalog

This Go connector lists authorized overseas warehouse codes, names, and countries through the [Chukou1 OpenAPI](https://openapi.chukou1.cn/v1/Help/Api/GET-v1-warehouses). It excludes orders, inventory, addresses, and customer information. Chukou1 is operated in China by [广州市贝法易信息科技有限公司](https://www.chukou1.cn/Default.aspx).

`ListWarehouses` calls `GET https://openapi.chukou1.cn/v1/warehouses` with a protected Bearer token. The [official API contract](https://openapi.chukou1.cn/v1/Help/Api/GET-v1-warehouses) defines the response array, while the [calling guide](https://openapi.chukou1.cn/v1/Help) defines HTTPS and authorization. Self-developed integrations can create a fixed token under Account > Customer Authorization Management, as the [self-authorization guide](https://openapi.chukou1.cn/v1/Help/SelfAuthorization) shows. Third-party ERP apps use a separate OAuth2 approval and consent process. HTTP 429, 5xx, and transport failures retry; definite authorization errors fail the Flow.

The [runnable Dex Flow](go/examples/warehouse-list/README.md) shows setup and typed output. The logo embeds the [official Chukou1 site logo](https://pic.chukou1.com/0/0_c5e81d.png) unchanged. Live authorization and production warehouse responses remain unverified.

## TypeScript package

The `@superdurable/connector-chukou1` package implements `listWarehouses` with the same request and outcomes as Go. Its [runnable warehouse list Flow](typescript/examples/warehouse-list/README.md) passed a real local Dex Worker, Client, retry, and persistence test against a provider stand-in. Twelve Go-recorded scenarios verify exact provider requests and outcomes. Live Chukou1 account access remains unverified.
