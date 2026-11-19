# StepFun TypeScript connector

`@superdurable/connector-stepfun` implements the read-only `listModels` Step. It calls the fixed `GET /v1/models` endpoint and returns each model ID, creation time in Unix seconds, and owner. It does not call inference.

Use the [authorization guide](../README.md#authorization) to configure the static `stepfun` connection in Dex Web. The [runnable Flow and Worker](examples/model-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
