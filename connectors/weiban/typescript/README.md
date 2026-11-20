# Weiban TypeScript connector

`@superdurable/connector-weiban` provides the read-only `listTags` Dex Query. It obtains a short-lived access token per call and returns up to ten tag IDs, names, groups, and the total count without customer records.

The [runnable Flow and Worker](examples/basic/) use the static `weiban` connection. Configure Corp ID and Secret through Dex Web Connections using the [authorization guide](../connector.yaml). The API origin, timeout, and response limit have manifest defaults.

Go-recorded conformance scenarios cover both token and tag requests, provider branches, and retries. The integration test exercises a real Dex Worker and Client with a provider fake returning 503 before success. Live Weiban authorization remains unverified without a user-controlled account.
