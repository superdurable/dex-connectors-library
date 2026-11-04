# OpenAI Connector for TypeScript

`@superdurable/connector-openai` exposes the stored Responses API as two Dex operations: `createResponse` and `retrieveResponse`. Create is a Mutation with a heartbeat checkpoint. It reads back a known response ID after a retry, and returns uncertainty when an unconfirmed write cannot safely be repeated. Retrieve is a Query. Both operations resolve the connection's API key before each request.

Install the package with `npm install @superdurable/connector-openai`. Dex TypeScript `@superdurable/dex` and `@superdurable/connectors-sdk` are peer application dependencies; the connector pins the Connector SDK to `0.3.0`.

When a streamed `createResponse` exceeds its configured body limit, the connector drains the provider stream before reading back a known response ID. This lets the provider finish creating the response before the read-back request.

The [runnable summarize-text Flow](examples/summarize-text/workflow.ts) binds the static `openai-api` connection with `newProjectConnection`, maps a safe request to `CreateRequest`, and wires only the `completed` branch. Run its Worker with `npm run example:summarize` after configuring the connection in Dex Web. `DEX_FLOW_SERVICE_ADDRESS` selects the Dex server; the default is `127.0.0.1:8801`.

The generated TypeScript request names retain Go's JSON field names (`Model`, `Input`, `Instructions`, `StructuredOutput`). The provider body uses the Responses API field names. Set `Model` to an empty string to use the connection model. `StructuredOutput` is optional; when present, provide `name`, `schema`, and `strict`. The connector also exports `newCreateResponseStep`, `newRetrieveResponseStep`, and `Mock` from the `/mock` subpath. The mock checks create input and strict structured answers using the SDK's portable JSON Schema rules.

Provider replay is covered by `test/conformance.test.ts`; the Dex Worker, Client, and Flow path is covered by `test/integration/summarize.integration.test.ts` against a local stand-in provider. Live OpenAI authorization and behavior have not been verified.
