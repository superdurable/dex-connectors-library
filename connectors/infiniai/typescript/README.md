# InfiniAI TypeScript connector

`@superdurable/connector-infiniai` implements the read-only `listModels` Step. It calls the fixed `GET /maas/v1/models` endpoint and returns each model ID, type, context token limit, and output token limit. It does not call inference.

Use the [authorization instructions](../README.md) to configure the static `infiniai` connection in Dex Web. The [runnable Flow and Worker](examples/model-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
