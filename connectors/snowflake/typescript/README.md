# Snowflake TypeScript connector

`@superdurable/connector-snowflake` supplies `submitStatement`,
`getStatementResult`, and `cancelStatement` for Dex TypeScript Flows. Use Node.js
22 or 24 with Dex TypeScript SDK 1.28.0 or later. Generated operation factories,
models, connection helpers, and mocks come from `connector.yaml` and the Go
models; the client and operation invocations are hand-written.

```sh
npm install @superdurable/connector-snowflake @superdurable/connectors-sdk @superdurable/dex
```

Configure the static Snowflake connection in Dex Web. The
[connector README](../README.md#connection-setup) describes its account,
warehouse, role, database, schema, statement timeout, row limit, byte limit,
and both authorization methods. At Worker startup, call
`loadFromEnvironment()` and `newProjectConnection(project, ConnectionName)` as
the [runnable example](examples/account-usage-summary/main.ts) does. The
credential provider resolves the current key or token before every request.

`submitStatement` sends one parameterized SQL statement with `async=true`, a
stable Dex Call ID as `requestId`, and `retry=true`. Values use typed bindings
outside SQL text. `getStatementResult` reads one partition and bounds retained
rows by `maxRows` and `maxResponseBytes`; `cancelStatement` cancels a queued or
running handle. The generated Step factories expose the happy branches
`submitted`, `completed`, and `canceled`. Optional branches are unwired unless
the Flow has a business reaction. Snowflake status code and SQLSTATE appear in
safe receipt metadata; credentials and provider messages do not enter Flow
state.

TypeScript numbers are limited to JavaScript's exact integer range. Use strings
for wide decimal values in application logic. The SQL API result mapping is in
the [connector README](../README.md#result-type-mapping).

From this package directory, run `npm ci && npm run check`. With `dexcli dev`
running on a free port, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address and
run `npm run integration:account-usage-summary`. This runs the checked-in Flow
through a real Dex Worker and Client with generated operation mocks. The
conformance suite replays Go-recorded HTTP scenarios for every operation,
branch, Retry classification, and transport failure.

Live Snowflake account authorization, JWT acceptance, SQL execution, result
partition delivery, and cancellation were not verified with a real provider.
