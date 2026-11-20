# ZTO TypeScript connector

`@superdurable/connector-zto` provides the read-only `queryWaybillTrace` Dex Query. It signs the JSON waybill request with ZTO AppKey and AppSecret and returns only scan type, scan timestamp, and scan site.

The [runnable Flow and Worker](examples/tracking/) use the static `zto` connection. Configure AppKey and AppSecret through Dex Web Connections using the [ZTO application guide](../connector.yaml). The endpoint, timeout, and response limit have manifest defaults.

Go-recorded conformance scenarios cover the provider branches and retries. The integration test exercises a real Dex Worker and Client with a provider fake that returns 503 before succeeding. Real ZTO authorization and parcel tracking remain unverified without a user-controlled account.
