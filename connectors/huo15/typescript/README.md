# Huo15 TypeScript connector

`@superdurable/connector-huo15` implements the read-only `listModels` Step. It calls the fixed `GET /v1/models` endpoint and returns the provider's total model count and at most 50 model IDs. It does not call inference.

Use the [authorization instructions](../README.md) to configure the static `huo15` connection in Dex Web. The [runnable Flow and Worker](examples/model-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
