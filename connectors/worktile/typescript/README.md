# Worktile TypeScript connector

`@superdurable/connector-worktile` provides the read-only `listProjects` Dex Query. It obtains a short-lived tenant token per call and returns a bounded project page without storing the token or personal information.

The [runnable Flow and Worker](examples/basic/) use the static `worktile` connection. Configure Client ID and Client Secret through Dex Web Connections using the [authorization guide](../connector.yaml). Timeout and response limit have manifest defaults.

Go-recorded conformance scenarios cover both token and project requests, provider branches, and retries. The integration test exercises a real Dex Worker and Client with a provider fake returning 503 before success. Live Worktile authorization remains unverified without a user-controlled tenant.
