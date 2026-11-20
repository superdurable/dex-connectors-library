# iFLYTEK Spark TypeScript connector

`@superdurable/connector-iflytek` implements the read-only `createChatCompletion` Step. It sends a bounded prompt to `POST /v1/chat/completions` and returns the assistant answer with token usage. The APIPassword stays in the credential provider and never enters Flow state.

Use the [authorization instructions](../README.md) to configure the static `iflytek` connection in Dex Web. The [runnable Flow and Worker](examples/chat-completion/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
