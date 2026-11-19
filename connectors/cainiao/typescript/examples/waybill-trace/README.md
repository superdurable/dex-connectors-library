# Cainiao waybill trace example

`flow.ts` runs `queryWaybillTrace` with the static `cainiao` Dex Web connection, then persists only the trace. The Flow input contains carrier code and waybill code. Configure TOP AppKey, AppSecret, and merchant SessionKey through the connector's Dex Web authorization guide.

Run `dexcli dev -open=false`, then `npm run example:worker` in one terminal and `npm run example:start -- YTO 12345` in another. For local FDG analysis before this package is published, run `mkdir -p node_modules/@superdurable && ln -s ../.. node_modules/@superdurable/connector-cainiao` once from the TypeScript package directory. Generate FDG 2.0 with `dexcli visualize --open=false --schema-version 2.0 examples/waybill-trace/flow.ts`.

The integration test uses a local provider fake; real merchant authorization and provider responses remain unverified.
