# NewsAPI TypeScript connector

`@superdurable/connector-newsapi` exposes NewsAPI's read-only `searchArticles`
operation to Dex TypeScript applications. It returns a typed `ArticlePage` and
selects `searched`, `providerRejected`, `invalidResponse`, or `defect`.

## Install

```sh
npm install @superdurable/connector-newsapi@0.1.0
```

The package requires the released `@superdurable/connectors-sdk` version pinned
in `package.json`. Use Node.js 22 or 24 for the Dex Worker.

## Configure the connection

Create a NewsAPI key at [newsapi.org/register](https://newsapi.org/register),
then find it under **Your API key** at
[newsapi.org/account](https://newsapi.org/account). In Dex Web **Connections**,
save it in the secret `api_key` field for the `newsapi` connection. Leave
`baseUrl`, `timeout`, and `maxResponseBytes` blank to use their displayed
defaults. The key is resolved for every provider call and sent only in the
`X-Api-Key` header. Never put it in Flow input or an Attribute.

Load the project once with `loadFromEnvironment()` from
`@superdurable/connectors-sdk/projectconfig` and open the connector's static
`ConnectionName` with `newProjectConnection(project, ConnectionName)`. The
TypeScript [company-news example](examples/company-news/README.md) uses the
same name to map a persisted request to one title search. Its runnable Worker
entrypoint contains:

```ts
const project = await loadFromEnvironment();
const connection = newProjectConnection(project, ConnectionName);
const flow = new CompanyNewsFlow(connection);
await runCompanyNewsWorker(flow, shutdown.signal, process.env.DEX_FLOW_SERVICE_ADDRESS || '127.0.0.1:8801', process.env.DEX_BLOB_CACHE_DIR || join(tmpdir(), 'dex-newsapi-company-news-blobs'));
```

The snippet is from [main.ts](examples/company-news/main.ts). The Flow uses
`newSearchArticlesStep` with `SearchArticlesBranchSearched` routed to its
terminal Step; other optional branches fail the Flow. See the complete
[flow.ts](examples/company-news/flow.ts) for its registered Attributes and
the fixed search window.

For a direct test connection, construct `Client` with settings and a credential
provider, then bind it with `newConnection(client, { provider: 'newsapi', name:
'newsapi' })`. The scripted `Mock` uses `newConnectionWithOperations` with the
same connection reference.

Dex CLI and Dex Web 1.28.0 support TypeScript FDG and connector release
identity. This package includes generated `dex-connector.json`. Strict
visualization of the example's local-to-connector route needs Dex CLI 1.28.1
(tracked in issue #362); configure the same named connection in project
configuration outside the Flow until that path can be verified. Do not put the
API key in the start request.

## Search contract

`SearchArticlesInput` requires `query`, `sources`, or `domains`. `query` is at
most 500 Unicode characters; `sources` accepts up to 20 provider source IDs.
`searchIn` accepts `title`, `description`, and `content`; `language` accepts
NewsAPI's documented two-letter codes. `pageSize` is 1 through 100, with zero
using NewsAPI's default of 100. `page` is one-based, with zero selecting page
one. Dates accept `YYYY-MM-DD`, UTC timestamps without an offset, or RFC 3339
timestamps. Omitted fields are not sent, leaving NewsAPI's defaults intact.

The Query uses async durability, a 30-second Execute timeout, and unlimited
attempts within the Connector SDK's one-hour retry policy. 429 and 408/5xx
responses retry; the `Retry-After` header sets a minimum delay. A conclusive
provider rejection selects `providerRejected`. A malformed or oversized 200
response selects `invalidResponse`. Invalid local input or credentials select
`defect` before a request. Optional branches are unwired by default and fail
the Flow when selected. The operation has no Mutation uncertainty path.

Article text is untrusted provider content; escape it before rendering HTML.
Nullable author, description, image, content, and source ID fields preserve
NewsAPI nulls. Publication timestamps are normalized to UTC.

## Mocks and verification

`@superdurable/connector-newsapi/newsapimock` supplies `Mock` with the
manifest's `searchedDefaultAnswer` installed. Use
`mock.searchArticles.respond(searchArticlesInvalidKey())` for a provider
rejection, or `searchArticlesRetry(...)` for a retried failure. The mock uses
the Connector SDK's per-Flow scripts and call log.

From this directory, run `npm ci && npm run check` for strict type checking,
linting, provider tests, a manifest drift test, and the ESM build. Start
`dexcli dev`, then run `npm run integration:company-news` to verify the example
through a real Dex Worker and Client with scripted provider answers. No live
NewsAPI key is required. Real provider authorization, rate limits, and article
results have not been verified in this package's automated tests.
