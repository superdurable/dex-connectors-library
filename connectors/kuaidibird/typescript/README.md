# KuaidiBird TypeScript connector

`@superdurable/connector-kuaidibird` provides the read-only `queryTracking` Dex Step for one carrier waybill. It signs the request with the KuaidiBird API key and returns the provider's tracking state and ordered events.

The [runnable Flow and Worker](examples/tracking/) use the static `kuaidibird` connection. At the [KuaidiBird console](https://www.kdniao.com), open 我的服务 > 用户信息 and copy the EBusinessID and paired AppKey into Dex Web Connections. Use a carrier code from the official carrier table and its waybill number. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider that returns 503 before succeeding. Real KuaidiBird authorization and parcel tracking remain unverified without a user-controlled account.
