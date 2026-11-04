# Select Supabase rows

`SelectRowsFlow` reads one bounded page through the static `supabase`
connection. The provider request runs in the connector Step; the Flow persists
only returned rows. Optional failure branches are unwired, so a rejection
fails the Flow.

Start `dexcli dev` on unused ports and set `DEX_FLOW_SERVICE_ADDRESS` to its
gRPC address. Configure the Dex Web connection named `supabase` with the
project REST URL ending in `/rest/v1` and a server-side secret API key. Set
`DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file.
From `connectors/supabase/typescript`, run:

```sh
npm ci
npm run build
npm run example:select
```

In another terminal with the same environment, edit `input.example.json` to
name an exposed table, then start the Flow:

```sh
npm run example:select:start -- examples/select-rows/input.example.json
```

The starter prints the selected rows. No secret belongs in the input file.

For local end-to-end verification with a provider stand-in on a free port:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:28811 npm run test:integration
```

Generate and lint the strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or
newer, after making the local package visible to the analyzer:

```sh
npm run build
npm install --prefix examples/select-rows --no-save --no-package-lock --ignore-scripts .
cd examples/select-rows
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Supabase project permissions and response variants remain unverified.
