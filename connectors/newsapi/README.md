# NewsAPI Connector

Search recent news articles through NewsAPI's
[everything endpoint](https://newsapi.org/docs/endpoints/everything) with a
typed Dex Query Step. Use the [company news example](examples/company-news/README.md)
to collect the newest headlines that name one company.

Its Go module is `github.com/superdurable/dex-connectors-library/connectors/newsapi`.
The connector ID is `newsapi`.

## Operation

| Factory | Input | Required branch | Optional branches |
| --- | --- | --- | --- |
| `NewSearchArticlesStep` | `SearchArticlesInput` | `searched` | `providerRejected`, `invalidResponse`, `defect` |

`SearchArticlesInput` sends only the fields an application sets, so NewsAPI
applies its own defaults to the rest:

| Field | NewsAPI parameter | Accepted values |
| --- | --- | --- |
| `Query` | `q` | up to 500 characters; quoted phrases, `+`, `-`, `AND`, `OR`, `NOT`, and parentheses |
| `SearchIn` | `searchIn` | `title`, `description`, `content`; empty searches all three |
| `Sources` | `sources` | up to 20 lowercase NewsAPI source IDs, such as `bbc-news` |
| `Domains`, `ExcludeDomains` | `domains`, `excludeDomains` | domain names such as `techcrunch.com` |
| `From`, `To` | `from`, `to` | a date such as `2026-01-05`, a UTC time such as `2026-01-05T07:00:00`, or an RFC 3339 time |
| `Language` | `language` | `ar de en es fr he it nl no pt ru sv ud zh`; empty returns every language |
| `SortBy` | `sortBy` | `publishedAt` (NewsAPI's default), `relevancy`, `popularity` |
| `PageSize`, `Page` | `pageSize`, `page` | 1 through 100 (zero uses NewsAPI's 100), and a 1-based page (zero is page 1) |

`Query`, `Sources`, or `Domains` must be set. The connector form-encodes every
value, so a company name such as `AT&T` or `#1 + Co` reaches NewsAPI intact.

The result is an `ArticlePage` with `TotalResults`, `Articles`, `Page`, and
`HasMore`. Each `Article` keeps NewsAPI's fields. `Author`, `Description`,
`URLToImage`, `Content`, and `Source.ID` are pointers that stay nil when NewsAPI
sends null, so an application can tell a missing value from an empty one.
`PublishedAt` is UTC. NewsAPI keeps removed articles in results with the title
`[Removed]`; the connector returns them unchanged. `Title`, `Description`, and
`Content` are untrusted provider text: escape them before rendering HTML.

Use the generated factory in [the runnable Flow](examples/company-news/flow/workflow.go).
`NewProjectConnection` opens the connection from the project configuration that
`projectconfig.LoadFromEnvironment` loads. `New` and `NewConnection` build a
connection for tests, and `WithHTTPClient` supplies a transport.

## Execution contract

`searchArticles` is a read-only Query with async durability, a 30-second Execute
timeout, and three attempts within two minutes, with exponential backoff from 1
second to 30 seconds. Each attempt makes one HTTP request. Redirects are not
followed.

| NewsAPI response | Outcome | Failure kind |
| --- | --- | --- |
| 200 with `status: ok` | `searched` | none |
| 401, or `apiKeyInvalid`, `apiKeyDisabled`, `apiKeyMissing` | `providerRejected` | `AUTHENTICATION` |
| `apiKeyExhausted` | `providerRejected` | `QUOTA_EXHAUSTED` |
| `parameterInvalid`, `parametersMissing`, `sourcesTooMany`, `sourceDoesNotExist` | `providerRejected` | `VALIDATION` |
| 426, such as `maximumResultsReached` or a date older than the plan allows | `providerRejected` | `AUTHORIZATION` |
| 429 `rateLimited` | Retry, honoring `Retry-After` | `RATE_LIMIT` |
| 408, 5xx, transport failure, interrupted body | Retry | `AVAILABILITY` |
| any other status | `providerRejected` | `PROVIDER_REJECTION` |
| malformed JSON, a non-`ok` status, missing fields, more articles than the page size, an oversized body | `invalidResponse` | `PROTOCOL` or `RESPONSE_TOO_LARGE` |

Invalid input, a missing key, or a key that cannot travel in one HTTP header
selects `defect` before a request is sent. A Failure names only NewsAPI's
documented error code, never NewsAPI's message text. An unwired optional branch
fails the Flow. The connector has no Mutation, idempotency key, or Trigger.

The free Developer plan allows 100 requests a day for development only, delays
articles by 24 hours, and searches about one month back. A daily-quota
`rateLimited` response retries within the two-minute window and then fails the
Step, so route Execute failure when a Flow must record that outcome.

| Configuration | Default | Meaning |
| --- | --- | --- |
| `baseUrl` | `https://newsapi.org/v2` | API base URL; HTTPS, or HTTP for a loopback test server |
| `timeout` | `15s` | HTTP request timeout |
| `maxResponseBytes` | `2097152` | Maximum response body size in bytes |

## Security

The API key is a `secretString` credential resolved for every call, so a
replaced key applies without restarting the Worker. It travels only in the
`X-Api-Key` header and never in a URL, Result, Failure, receipt, or log. Create
the key at [newsapi.org/account](https://newsapi.org/account) and store it only
in Dex Web **Connections**.

## Verification

From `connectors/newsapi`:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/company-news/... -count=1
```

The provider tests cover the header-only key, query encoding, null-preserving
article fields, removed articles, every documented error code, input
validation, malformed and oversized responses, transport failure, and credential
replacement. The example's real Dex suite covers the found and wired rejected
routes, a retried outage, an unwired `invalidResponse`, and invalid start input.

An opt-in live test makes two NewsAPI requests, which count against the key's
daily allowance: one with a wrong key, which must select `providerRejected`
with `AUTHENTICATION`, and one three-article title search:

```bash
NEWSAPI_CONNECTOR_TEST_API_KEY=... GOWORK=off go test -tags=live -run TestLive ./...
```
