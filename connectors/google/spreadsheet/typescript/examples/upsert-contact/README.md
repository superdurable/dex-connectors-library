# Upsert a Google Sheets contact

This Flow uses the static Dex Web connection `google-sheets-contacts`. In Dex
Web Connections, create or select a Google OAuth Web application from
https://console.cloud.google.com/apis/credentials, enable the Sheets and Drive
APIs, register Dex's displayed redirect URI, and authorize `drive.file`.
The existing connector's setup UI selects a spreadsheet and tab. Save the
connection outside this Flow; no token belongs in the input file.

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address, and set
`DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json`. From this
package directory, run `npm run example:upsert` for the Worker. In another
terminal, edit `examples/upsert-contact/input.example.json` for the chosen
spreadsheet, then run
`npm run example:upsert:start -- examples/upsert-contact/input.example.json`.

The starter prints the confirmed row locator. The package's integration test
uses the same Flow with a stand-in Sheets API and real Dex Worker and Client:
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration`.

Generate a strict Flow Definition Graph 2.0 from this source with Dex CLI 1.28.0
or newer. After `npm run build`, make the package visible to the analyzer with
`npm install --prefix examples/upsert-contact --no-save --no-package-lock --ignore-scripts .`,
then from `examples/upsert-contact/` run
`dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false`.
Live Google OAuth and Sheets behavior have not been exercised.
