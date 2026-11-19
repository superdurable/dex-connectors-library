# Smartsheet TypeScript connector

The `@superdurable/connector-smartsheet` package provides the `listSheets` and
`getSheet` Dex Queries. Use a Dex Web connection named `smartsheet` with an API
key from **Smartsheet > Account icon > Personal Settings > API Access**. The
connection keeps the secret outside Flow input and state.

`newProjectConnection(project, 'smartsheet')` loads the configured `baseUrl`,
`timeout`, and `maxResponseBytes` values and resolves the API key on each call.
The generated `newListSheetsStep` and `newGetSheetStep` factories expose the
manifest branches. The [runnable example](examples/smartsheet-lookup/flow.ts)
lists a page and reads its first sheet through a Dex Worker. Cell values retain
provider JSON number text, including decimal notation and integers beyond
JavaScript's safe integer range when the client response is serialized directly.
The Dex JSON codec parses cell values into JavaScript numbers at the Flow
boundary, so Flow output does not retain numeric text or unsafe integers.
Sheet, row, and column IDs use the generated `number` type and must be within
JavaScript's safe integer range; larger int64 IDs cannot be represented exactly
by this package.

Run `npm ci && npm run check` in this directory. To verify the example against
a local Dex stack, start `dexcli dev -dex-port 28501 -web-port 28502 -open=false`
and run `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:28501 npm run integration:smartsheet-lookup`.
The integration uses a local Smartsheet stand-in that binds a free port and
checks the retry, both provider requests, and row delivery. Real Smartsheet
authorization and live provider responses remain unverified.
