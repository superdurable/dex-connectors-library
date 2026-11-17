# ALAPI IP lookup connector

The [ALAPI IP lookup API](https://www.alapi.cn/api/23/api_document) accepts an IP address and returns country, province, city, district, and ISP. This connector queries public IPv4 addresses through the documented HTTPS `POST /api/ip` endpoint and returns only those coarse fields. ALAPI is operated by [南昌凌侠科技有限公司](https://www.alapi.cn/page/privacy_policy).

Sign in at [ALAPI](https://www.alapi.cn/), enable **IP地址查询**, then open 控制台 > 数据中心 > token管理 to copy an authorized token. Store it in Dex Web Connections. The token is sent in the `token` header and is excluded from request bodies, Flow results, and errors. Queries may consume provider quota. The [official OpenAPI contract](https://v3.alapi.cn/openapi/23.json) defines the request and response.

Run the [example Flow](go/examples/lookup-ip-location/) with its generated mock or a user-controlled connection. Pass only public IPv4 addresses you are authorized to query. Provider errors, malformed data, oversized bodies, and retryable transport failures have separate outcomes. Real provider authorization and billing have not been verified locally.

The [Python package](python/) implements the same `lookupIPLocation` Query and includes a [runnable Flow](python/examples/ip_location/) with a Worker entrypoint. Its Go-recorded conformance scenarios cover success, rejected credentials or quota, malformed responses, invalid input, 429, 503, and transport failure. The Python integration test ran against a local Dex stack with a fake provider; live ALAPI authorization and quota consumption remain unverified.

The [TypeScript package](typescript/README.md) implements the same `lookupIPLocation` Query with Go-recorded conformance scenarios and a runnable Dex Flow.
