# Yunda TypeScript connector

`@superdurable/connector-yunda` provides the read-only `getWaybillBalance` Dex Step. It sends the fixed Yunda Baima balance request and returns only free and paid ordinary waybill counts. The [Yunda API reference](https://yundaex.apifox.cn/api-7039547) describes the request.

Use the [runnable Flow and Worker](examples/waybill-balance/) with the static `yunda` connection. In Dex Web Connections, create or select an application at [Yunda Open Platform](https://open.yundaex.com/) > Applications, then enter its app-key and app-secret. Obtain the Baima partner ID and integration secret from your cooperating Yunda outlet. All four values are private and are resolved for each call; leaving one blank prevents a request. The endpoint and request type are fixed by the connector.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client against a local provider that returns 503 before succeeding. Real Yunda authorization and Baima account behavior remain unverified without a user-controlled connection.
