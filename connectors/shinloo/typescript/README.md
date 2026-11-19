# Shinloo TypeScript connector

`@superdurable/connector-shinloo` implements the read-only `listModels` Step. It calls the fixed `GET /v1/models` endpoint and returns the provider model count and at most fifty IDs. Inference and account data are excluded.

Use the [authorization guide](../README.md#authorization) to configure the static `shinloo` connection in Dex Web. The [runnable Flow and Worker](examples/model-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
