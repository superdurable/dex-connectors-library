# Hacker News Daily Connector

Read public Hacker News feeds, stories, and comments through typed Dex Query
Steps. Use the [daily digest example](examples/daily-digest/README.md) to monitor
developer topics and produce summaries with direct source links.

The connector uses the [official Hacker News API](https://github.com/HackerNews/API).
It requires no API key. Its Go module is
`github.com/superdurable/dex-connectors-library/connectors/hackernews`.
The connector ID is `hacker-news-daily`. The first release is `v0.1.0`.

## Operations

| Factory | Input | Required branch | Optional branches |
| --- | --- | --- | --- |
| `NewListStoryIDsStep` | `ListStoryIDsInput` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `NewGetItemStep` | `GetItemInput` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`ListStoryIDs` reads `top`, `best`, `new`, `ask`, or `show`. Empty `Feed` selects
`top`. `Limit` accepts 1 through 500; zero selects 50. The result preserves the
provider's order and reports `ObservedAt` and `HasMore`. These feeds are current
snapshots. They do not provide pagination or a complete historical daily window.
The top feed can include jobs.

`GetItem` reads one positive item ID. It returns the provider's public fields,
including ranked child IDs, and an exact `DiscussionURL`. `TextHTML` contains
the submitted story or comment text. It does not contain a linked article.
Hacker News does not expose comment scores. A null, deleted, dead, or HTTP 404
item selects `notFound`. The operation does not fetch children automatically.
Applications must escape or sanitize provider text before rendering HTML.

Use the generated factories in [the runnable Flow](examples/daily-digest/flow/workflow.go).
`NewLocalConnection` loads a Dex Web connection. `New` and `NewConnection` support
applications that provide their own configuration. `WithHTTPClient` supplies a
transport for tests or application networking.

## Execution contract

Both operations are read-only queries. Each Execute attempt makes one HTTP
request. They use async durability and a 30-second Execute timeout. HTTP 408,
429, 5xx, transport failures, and interrupted response bodies retry. The default
policy allows five attempts within 65 minutes, with exponential backoff from
1 second to 30 seconds. `Retry-After` can extend the delay up to one hour, within
the remaining retry budget. Other non-200 responses select `providerRejected`.
Redirects are not followed.

Malformed JSON, mismatched item IDs, unknown item types, invalid child IDs, and
oversized bodies select `invalidResponse`. Invalid operation input selects
`defect` before a request is sent. An unwired optional branch fails the Flow.
The connector has no mutation, idempotency key, Trigger, or external write.

| Configuration | Default | Meaning |
| --- | --- | --- |
| `baseUrl` | `https://hacker-news.firebaseio.com/v0` | API base URL; HTTPS or loopback HTTP |
| `timeout` | `15s` | HTTP request timeout |
| `maxResponseBytes` | `1048576` | Maximum response body size in bytes |

The constructor validates configuration. It does not resolve or transmit
credentials. The public API client is safe for concurrent calls.

## Daily digest behavior

The example owns scheduling, preferences, retained candidates, and publication
history. It samples hourly and publishes every 24 hours. It uses the locally
authenticated Codex CLI to select and summarize evidence. The connector itself
has no model dependency. The example keeps its latest report in Dex and exposes
it in Dex Web. It does not send email or chat messages.

Every entry places its original link and HN discussion link after the summary.
An entry that cites a sampled comment also includes that comment's permalink.
The application builds these links from API records. The model returns IDs and
text, and cannot supply citation URLs.

## Verification

Run checks from the repository root:

```sh
make test-connectors
make catalog-check
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 make test-connector-integration
GOTOOLCHAIN=go1.24.0 make test-dex-compat-current
make check
```

The provider tests cover feed order and limits, item parsing, removal, HTTP
failure classification, redirects, malformed responses, and size bounds.
The example tests use a real Dex Server for retries, deduplicated reads,
default English, source links, durable waiting, and Worker replacement.
They use deterministic summary output and do not require Codex credentials.
