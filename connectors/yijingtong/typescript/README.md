# Yijingtong TypeScript connector

`@superdurable/connector-yijingtong` provides the read-only `listWarehouses` Dex Query. It returns authorized warehouse codes, names, and countries without inventory or customer details.

The [runnable Flow and Worker](examples/basic/) use the static `yijingtong` connection. Configure the tenant HTTPS system origin and token through Dex Web Connections using the [authorization guide](../connector.yaml). Timeout and response limit have manifest defaults.

Go-recorded conformance scenarios cover provider branches and retries. The integration test exercises a real Dex Worker and Client with a provider fake returning 503 before success. Live Yijingtong authorization remains unverified without a user-controlled account.
