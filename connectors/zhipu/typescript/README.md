# Zhipu TypeScript connector

`@superdurable/connector-zhipu` provides the `generateText` Dex Query for one nonstreaming GLM text response. It returns text, finish reason, response ID, model, and provider token usage without storing the API key.

The [runnable Flow and Worker](examples/basic/) use the static `zhipu` connection. Configure the API key through Dex Web Connections using the [authorization guide](../connector.yaml). The model, timeout, and response limit have manifest defaults. Generation may be billed even if a retry is needed.

Go-recorded conformance scenarios cover complete, truncated, blocked, rejected, and retry outcomes. The integration test exercises a real Dex Worker and Client with a provider fake returning 503 before success. Live Zhipu authorization and model generation remain unverified without a user-controlled account.
