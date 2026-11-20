# STO TypeScript connector

`@superdurable/connector-sto` provides the read-only `queryWaybillTrace` Dex Query. It signs a waybill number with the STO application secret and returns scan type, time, and branch name without provider personal details.

The [runnable Flow and Worker](examples/tracking/) use the static `sto` connection. Configure AppKey, AppSecret, and FromCode through Dex Web Connections using the [STO authorization guide](../connector.yaml). The gateway URL, timeout, and response limit have manifest defaults.

Go-recorded conformance scenarios cover provider branches and retries. The integration test exercises a real Dex Worker and Client with a provider fake returning 503 before success. Real STO authorization and tracking remain unverified without a user-controlled account.
