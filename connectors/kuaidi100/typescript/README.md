# Kuaidi100 TypeScript connector

`@superdurable/connector-kuaidi100` provides the read-only `queryTracking` Dex Step for a carrier waybill. It signs the request with the customer code and key, then returns the provider's current state and latest-first events.

The [runnable Flow and Worker](examples/tracking/) use the static `kuaidi100` connection. At the [Kuaidi100 enterprise console](https://api.kuaidi100.com/login), enable Real-time Query, then open 我的信息 > 企业信息 and enter the paired customer code and key in Dex Web Connections. Use the official carrier code and waybill number. Some carriers also require the recipient or sender phone. The endpoint, timeout, and response limit have manifest defaults.

The checked-in conformance scenarios replay Go-recorded requests and outcomes. The integration test exercises a real Dex Worker and Client with a local provider. Real Kuaidi100 authorization and parcel tracking remain unverified without a user-controlled account. Provider policy requires at least 30 minutes between queries for one parcel, so the real Dex integration test verifies the successful path; Go replay verifies retry classification.
