# Contentful TypeScript connector

`@superdurable/connector-contentful` implements the read-only `getEntry` and
`listEntries` Dex Queries. The manifest generates models, Step factories,
branches, connection bindings, mock cases, and `dex-connector.json`; `src/client.ts`
owns the provider HTTP behavior.

## Setup

Install `@superdurable/connector-contentful@0.1.0` and the matching released
Connector SDK. In [Contentful](https://app.contentful.com), select a space,
open **Settings → API keys**, and create or select a Content Delivery API key.
Copy its access token into the secret `delivery_token` field in Dex Web
**Connections** for the `contentful` connection. Find the Space ID on the key
page; supply it and an environment ID in Flow input. Leave `baseUrl`, `timeout`,
and `maxResponseBytes` blank for the manifest defaults. Tokens never belong in
Flow input, Attributes, or logs.

The [sample application](examples/sample-contentful/README.md) loads this
connection from the Dex project and runs separate entry and page Flows. Its
Worker entrypoint uses the static name:

```ts
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const registry = new Registry([new GetEntryFlow(connection), new ListEntriesFlow(connection)]);
```

This snippet comes from [worker.ts](examples/sample-contentful/worker.ts).
The [Flow source](examples/sample-contentful/flow.ts) uses generated
`newGetEntryStep` and `newListEntriesStep` factories and wires only the success
branches. An unwired optional branch fails its Flow.

## Behavior and verification

IDs accept ASCII letters, digits, underscore, and hyphen. `listEntries.limit`
accepts 1–1000, and zero or omission uses Contentful's default; `skip` is a
nonnegative offset. A single-entry 404 selects `notFound`. Authentication and
other conclusive 4xx responses select `providerRejected`; invalid credentials
or input select `defect`. A malformed, incomplete, or oversized 200 response
selects `invalidResponse`. HTTP 408, 409, 429, 5xx, and transport failures
retry under the Connector SDK's default one-hour budget. Both operations are
Queries; they have no uncertainty path. Provider error bodies are not copied
to Failure messages.

From this directory, run `npm ci && npm run check`. With `dexcli dev` running,
set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`. The integration
test starts a free-port provider stand-in and exercises both Flows through a
real Dex Worker and Client, including a retry and an unwired rejection. The
conformance test replays the Go-recorded requests, branches, and outputs.
Live Contentful authorization, permissions, and response variants have not been
verified.
