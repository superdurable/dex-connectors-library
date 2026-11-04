# Cohere search example

The `CohereSearchTS` Flow embeds the search question and reranks at least two
candidate passages. Its static Dex Web connection name is `cohere`.

From `connectors/cohere/typescript`, run `npm ci` and `npm run build`. Configure
the Cohere connection in Dex Web with the API key from
https://dashboard.cohere.com/api-keys. Start `dexcli dev`, then run
`npx tsx examples/cohere-search/main.ts` with `DEX_FLOW_SERVICE_ADDRESS` set
to the Dex Flow service address. Start a `CohereSearchTS` Flow with:

```json
{"query":"release","documents":["weather report","release note"]}
```

The integration test runs this same Flow through a real Dex Worker and Client
using a local Cohere stand-in. It covers both provider Steps, a 503 retry, and
an unwired authentication rejection. Run it with `DEX_FLOW_SERVICE_ADDRESS`
pointing to a running `dexcli dev` instance, then `npm run test:integration`.

For FDG 2.0, run from this package directory:

```sh
dexcli visualize examples/cohere-search/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/cohere-search-ts
```

The key remains in Dex Connections; it is never part of the Flow input.
