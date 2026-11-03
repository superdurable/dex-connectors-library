# Alpha Vantage TypeScript connector

`@superdurable/connector-alphavantage` provides `getGlobalQuote` and
`getTimeSeriesDaily` Query operations for Dex TypeScript Flows. Both operations
resolve the API key for every call and preserve provider price and volume
strings. The package exports generated operation Step factories, definitions,
models, and scripted mocks through `alphavantagemock`.

Install with `npm install @superdurable/connector-alphavantage`. The
[runnable demo](examples/demo/README.md) shows the static `alphavantage`
connection and a real Dex Worker. It wires only `found`; optional branches
fail the Flow if selected. Provider reads retry on HTTP 429, 408, 5xx,
transport errors, and Alpha Vantage rate limit notices.

Configure Dex Web Connections with the secret `api_key` from
[Alpha Vantage API key support](https://www.alphavantage.co/support/api-key/).
The provider requires this key in the `apikey` URL query parameter; treat
HTTP request URL logs as secret-bearing. Leave `baseUrl`, `timeout`, and
`maxResponseBytes` blank for their manifest defaults. The package resolves
the key at call time and never stores it in Flow input or output.

From this directory, run `npm ci && npm run check`. Conformance tests replay
the Go recording at `conformance/connectors/alphavantage/scenarios.json`.
Run `npm run integration:demo` against `dexcli dev` for a credential-free,
real Worker and Client check. Live Alpha Vantage authorization, quota response
variants, and Dex Web form behavior remain unverified.
