# Huoban TypeScript connector

`@superdurable/connector-huoban` implements the read-only `listSpaces` Step. It calls the fixed `GET /openapi/v1/space/list` endpoint and returns authorized workspace IDs and names without reading records or member data.

Use the [authorization instructions](../README.md) to configure the static `huoban` connection in Dex Web. The [runnable Flow and Worker](examples/space-list/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
