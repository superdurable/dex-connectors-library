# Contentful sample Flows

`GetEntryFlow` and `ListEntriesFlow` use the static `contentful` Dex Web
connection. Create a Content Delivery API key at
[app.contentful.com](https://app.contentful.com) under **Settings → API keys**
and save its token as secret `delivery_token` in Dex Web **Connections**.
Set `DEX_CONNECTOR_CONFIG_FILE` to Dex Web's local `connections.json` file and
`DEX_FLOW_SERVICE_ADDRESS` to the running `dexcli dev` gRPC address.

From `connectors/contentful/typescript`, start the Worker:

```sh
npm ci
npm run example:worker
```

In another terminal with the same project configuration, start a single-entry
Flow with real IDs from your Contentful space:

```sh
npm run example:start -- space123 entry-meridian master
```

The starter prints the completed entry. The Worker also registers
`ContentfulListEntries`, which accepts `spaceId`, `environmentId`, optional
`limit`, and optional `skip`. No credential belongs in Flow input.

For deterministic verification, run `npm run test:integration` against a local
`dexcli dev` service. The test binds its provider stand-in to a free port and
checks both operations, retry, and an unwired rejection.

Generate and lint strict Flow Definition Graph 2.0 with Dex CLI 1.28.0 or
newer. From this package directory, make the package visible to the analyzer
and run:

```sh
npm run build
npm install --prefix examples/sample-contentful --no-save --no-package-lock --ignore-scripts .
cd examples/sample-contentful
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Live Contentful authorization and responses have not been exercised locally.
