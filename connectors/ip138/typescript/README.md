# iP138 TypeScript connector

`@superdurable/connector-ip138` implements the read-only `getTokenUsage` Step. It calls the fixed `GET /status/` endpoint with the private token and returns current-hour requests, hourly allowance, and package quota. The token never enters Flow state.

Use the [authorization instructions](../README.md) to configure the static `ip138` connection in Dex Web. The [runnable Flow and Worker](examples/token-usage/) use that connection. Credentials stay outside Flow state and are resolved for each call.

Go-recorded conformance tests check provider outcomes and exact requests. The integration test runs a real Dex Worker and Client with a local provider that returns 503 once before success. Authorization against a real provider account remains unverified.
