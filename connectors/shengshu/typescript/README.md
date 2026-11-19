# Vidu TypeScript connector

`@superdurable/connector-shengshu` implements the read-only `getTaskStatus` Step. It calls `GET /ent/v2/tasks/{taskId}/creations` and returns safe task status metadata, creation IDs, and credits consumed. Signed media URLs are excluded.

Use the [authorization guide](../README.md#authorization) to configure the static `shengshu` connection in Dex Web. The [runnable Flow and Worker](examples/task-status/) use that connection. The API key stays outside Flow state and is sent in the Token authorization header.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real Vidu account remains unverified.
