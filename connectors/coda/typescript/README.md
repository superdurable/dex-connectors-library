# Coda TypeScript connector

`@superdurable/connector-coda` provides the read-only `listRows` and
`getRow` operations for Dex TypeScript Flows. The generated operation
factories and models come from [the connector manifest](../connector.yaml);
`CodaClient` resolves the private API token for each call.

The [runnable list example](examples/list-rows/) uses the static `coda`
connection and calls `newListRowsStep`. Its happy-path `listed` branch
completes the Flow; an unwired optional branch fails it. The operation inputs
contain only document, table, and row IDs, page size, and page token. Do not
put an API token in Flow input.

Install the package with `npm install @superdurable/connector-coda`. To
prepare a local Worker, create a private Dex project connection named `coda`
for provider `coda`. The connection must supply `api_token`; its
`baseUrl`, `timeout` (a Go duration), and `maxResponseBytes` fields may use manifest
defaults. Direct `CodaClient` configuration accepts `timeoutMs` in milliseconds.
[The runnable Worker and starter](examples/list-rows/README.md)
load that connection through `newProjectConnection`.

Run the package checks from this directory with `npm ci && npm run check`.
The [Go-recorded scenarios](../../../conformance/connectors/coda/scenarios.json)
verify request paths, headers, query parameters, outputs, branches, and retry
classification in TypeScript. The real Dex integration test uses a local Coda
HTTP fixture. Live Coda authorization, document permissions, and response
variants have not been verified.
