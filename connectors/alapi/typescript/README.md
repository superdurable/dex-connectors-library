# ALAPI TypeScript connector

`@superdurable/connector-alapi` provides the `lookupIPLocation` read-only Dex Step. It accepts one public IPv4 address and returns coarse country, province, city, district, and network operator fields. Its request follows the [ALAPI IP address API](https://www.alapi.cn/api/23/api_document).

Use the [runnable Flow and Worker](examples/ip-location/) with a named `alapi` connection. In Dex Web Connections, enable IP地址查询, then add the secret token from ALAPI 控制台 > 数据中心 > token管理. The timeout and maximum response size have safe defaults in the connector manifest. Credentials are resolved for each call and never enter Flow state.

The checked-in conformance scenarios replay Go-recorded provider requests and every declared branch. The example's integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real ALAPI authorization, quota usage, and live responses remain unverified without a user-controlled token.
