# NewsAPI company-news example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordCompanyNewsRequest` validates the typed start input, applies
   defaults, fixes the search window once, and persists the request in the
   `newsapi-company-news-request` Attribute;
2. `SearchCompanyArticles` calls `newsapi.NewSearchArticlesStep` for the newest
   articles whose titles name the company;
3. `CompanyArticlesFound` records the page and completes the Flow, while
   `CompanyNewsRejected` completes without articles when NewsAPI rejects the
   search, such as an exhausted key.

Both completion Steps write the `newsapi-company-news-outcome` Attribute. The
summary and display RPCs show the request and the outcome in Dex Web. The
example leaves `invalidResponse` and `defect` unwired, so those outcomes fail
the Flow.

`RecordCompanyNewsRequest` computes `from` once, in UTC, and persists it, so a
retried search uses the same window. The start Step implements a `WaitFor` that
skips immediately because Dex Web Start Flow invokes the start Step's
`WaitFor`.

## Validate the Flow Definition

From `connectors/newsapi`, generate strict FDG 2.0 with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/company-news/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/company-news
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release.

## Configure and run

This example is part of the connector module, so its Flow Definition names the
local module rather than a published release. Dex needs release metadata built
from this source, passed as an override; without it the connection shows
**Unsupported**. The connector has no Studio bundle, so the metadata needs no UI
artifact. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/newsapi-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/newsapi/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/newsapi \
  --version v0.21.0 --tag connectors/newsapi/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/newsapi-release/connector-release.json \
  --digest-output /tmp/newsapi-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/newsapi/build" \
  --connector-release-override newsapi=/tmp/newsapi-release
```

Open the Dex Web URL that dexcli prints and select **Connections**. Select
`newsapi / newsapi`, which the `SearchCompanyArticles` Step of
`NewsAPICompanyNews` uses. Follow the guide to create a key at
[newsapi.org/register](https://newsapi.org/register), paste it from
[newsapi.org/account](https://newsapi.org/account) into `api_key`, leave
`baseUrl`, `timeout`, and `maxResponseBytes` blank, and save. The status
becomes **Ready**.

In a second terminal, start the Worker from `connectors/newsapi` with the
project's `DEX_PROJECT_*` environment, which
[`sdkgo/projectconfig`](../../../../sdkgo/projectconfig/README.md#application-loading)
documents:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/newsapi"
go run ./examples/company-news
```

The Worker listens on `127.0.0.1:8873`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. If the Dex
Server is unreachable, the Worker logs `dex server unavailable; retrying` and
starts once Dex answers. It logs the connection name, never the key.

In the Run workspace, choose **Start Flow**, select `NewsAPICompanyNews`,
choose the Worker at `127.0.0.1:8873`, enter a Flow ID, and submit:

```json
{
  "company": "OpenAI",
  "days": 10,
  "maxArticles": 20,
  "language": "en"
}
```

`days` defaults to 10 and accepts 1 through 30, `maxArticles` defaults to 20 and
accepts 1 through 100, and `language` defaults to `en`. The run completes with a
`CompanyNewsOutcome` whose `status` is `found` and whose `articles` hold the
newest matching headlines, and the run detail shows the same value under
`newsapi-company-news-outcome`. A blank company, `days` above 30, or
`maxArticles` above 100 fails the Flow before NewsAPI is called.

## Test

From `connectors/newsapi`:

```bash
GOWORK=off go test -race ./examples/company-news/...
```

The real Dex suite starts Flows through the Dex Client against a local fake
NewsAPI. It covers the found route, a retried HTTP 503, the wired rejection for
an exhausted key, an unwired `invalidResponse` that fails without a retry, and
invalid start input:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/company-news/... -count=1
```
