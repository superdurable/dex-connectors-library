# Close TypeScript connector

`@superdurable/connector-close` provides typed `listLeads` and `getLead`
read Queries for Dex. It resolves the secret API key from the named connection
on each call. The key never enters a Flow input, receipt, or result.

## Install and connect

```sh
npm install @superdurable/connector-close @superdurable/connectors-sdk @superdurable/dex
```

Create a Dex Web `close` connection named `close`. From
[Close Settings > Developer > API Keys](https://app.close.com/settings/api/),
create a token with read access and paste it into the secret `api_key` field.
Leave `baseUrl`, `timeout`, and `maxResponseBytes` blank for their manifest
defaults. See the [runnable lookup example](examples/close-lookup/README.md)
for Worker and starter files.

## Operations

`newListLeadsStep` reads a bounded page and `newGetLeadStep` reads a `lead_`
identifier. The example wires the happy-path `listed` and `found` branches.
An unwired optional branch fails the Flow. A 404 on `getLead` selects
`notFound`; definitive provider rejections select `providerRejected`;
malformed or oversized JSON selects `invalidResponse`; invalid local input or
credentials select `defect`. Rate limits, 408, 409, 5xx, and transport failures
retry under the Dex Step retry policy. Provider error bodies are never copied
into failures.

## Verification

```sh
npm ci
npm run check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:23740 npm run test:integration
```

The integration test needs a running `dexcli dev` at the address in
`DEX_FLOW_SERVICE_ADDRESS` and uses a local provider fixture on an assigned port.
The example Worker and starter also require `DEX_FLOW_SERVICE_ADDRESS`.
Recorded Go conformance scenarios verify
the provider request, response branches, retries, and exact SDK output.
Live Close authorization and provider responses have not been exercised.
