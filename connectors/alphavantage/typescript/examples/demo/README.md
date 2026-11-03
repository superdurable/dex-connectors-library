# Alpha Vantage quote demo, TypeScript

`AlphaVantageQuoteDemo` starts with `{ "symbol": "IBM" }`, queries the
`getGlobalQuote` operation, and returns the quote on `found`. The connection
name is the static `alphavantage` constant in [flow.ts](flow.ts). The
provider Step declares its group and explanation in the same file. Optional
branches remain unwired and fail the Flow.

From `connectors/alphavantage/typescript`, run `npm ci`. Start `dexcli dev`,
create the `alphavantage / alphavantage` connection in Dex Web, and enter the
secret `api_key` using the provider guide at
https://www.alphavantage.co/support/api-key/. With `DEX_PROJECT_*` environment
set, run `npm run example:demo`. In Dex Web, start `AlphaVantageQuoteDemo`
with a unique Flow ID and `{ "symbol": "IBM" }`.

For a credential-free check through a real Dex Worker, Client, and retry
boundary, run `npm run integration:demo` while `dexcli dev` is running. It
uses the generated mock with the same Flow and static connection name.

Generate the strict FDG 2.0 view from this directory:

```sh
dexcli visualize examples/demo/flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/alphavantage-typescript-demo
```

The command must report `valid: true`; an unpublished connector release may
instead report `connector_release_required` until release metadata is
available. Run `GOWORK=off go run ./cmd/connectorctl generate --check
connectors/alphavantage/connector.yaml` from the repository root to check
generated contracts.
