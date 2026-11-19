# Hacker News Daily Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/hackernews/go@v0.26.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/hackernews`.

Read public Hacker News feeds, stories, and comments, and search stories by
keyword, through typed Dex Query Steps. Use the
[daily digest example](go/examples/daily-digest/README.md) to monitor developer
topics and produce summaries with direct source links, and the
[story search example](go/examples/story-search/README.md) to find recent
stories that mention a keyword.

The connector uses the [official Hacker News API](https://github.com/HackerNews/API)
for feeds and items, and the [Hacker News Search API](https://hn.algolia.com/api)
linked from the Hacker News footer for search. It requires no API key. Its Go module is
`github.com/superdurable/dex-connectors-library/connectors/hackernews/go`.
The connector ID is `hacker-news-daily`.

The Python package is [`dex-connector-hackernews`](python/README.md), imported as
`dex_connectors.hackernews`.

The TypeScript package is [`@superdurable/connector-hackernews`](typescript/README.md).
Its [top-story Flow](typescript/examples/top-story/README.md) binds the static
`hackernews-public` connection and uses both generated Query Step factories.
The TypeScript package implements all three operations from v0.4.0. The
Python package implements `listStoryIDs` and `getItem`; `searchStories` is not
yet in Python. Go-recorded provider scenarios replay through each package for
the operations it implements.

## Operations

| Factory | Input | Required branch | Optional branches |
| --- | --- | --- | --- |
| `NewListStoryIDsStep` | `ListStoryIDsInput` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `NewGetItemStep` | `GetItemInput` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `NewSearchStoriesStep` | `SearchStoriesInput` | `searched` | `providerRejected`, `invalidResponse`, `defect` |

`ListStoryIDs` reads `top`, `best`, `new`, `ask`, or `show`. Empty `Feed` selects
`top`. `Limit` accepts 1 through 500; zero selects 50. Both fields are optional
in the input schema, so `{}` and `{"limit": 2}` are valid inputs. The result preserves the
provider's order and reports `ObservedAt` and `HasMore`. These feeds are current
snapshots. They do not provide pagination or a complete historical daily window.
The top feed can include jobs.

`GetItem` reads one positive item ID. It returns the provider's public fields,
including ranked child IDs, and an exact `DiscussionURL`. `TextHTML` contains
the submitted story or comment text. It does not contain a linked article.
Hacker News does not expose comment scores. A null, deleted, dead, or HTTP 404
item selects `notFound`. The operation does not fetch children automatically.
Applications must escape or sanitize provider text before rendering HTML.

`SearchStories` reads one page of the Search API's story results. Every input
field is optional:

| Field | Meaning |
| --- | --- |
| `Query` | Keyword query of at most 512 bytes, matched against titles, URLs, authors, and story text. Empty matches every story the filters allow |
| `SortBy` | `StorySearchSortRelevance` (`relevance`, the empty default) or `StorySearchSortDate` (`date`, newest first) |
| `Tags` | `StoryTagShowHN`, `StoryTagAskHN`, or `StoryTagFrontPage`; a story must carry every listed tag. `StoryTagStory` is always applied, so Ask HN and Show HN posts are included |
| `CreatedAfter`, `CreatedBefore` | Strict submission-time bounds; zero leaves a side open |
| `MinPoints` | Minimum points; zero sets none |
| `Page`, `HitsPerPage` | Zero-based page and page size from 1 through 100; zero selects 20. `Page` times `HitsPerPage` must stay below 1000 |

Each `StorySearchHit` carries the item `ID`, `Title`, `URL` (empty for text
posts), `DiscussionURL`, `Author`, `Points`, `CommentCount`, `CreatedAt` in
UTC, the story's known `Tags`, and `StoryText`: the submitted HTML as plain
text of at most `MaxStoryTextCharacters` (2000) code points, with
`IsStoryTextTruncated` set when it was cut. The page reports `Page`, `Pages`,
`TotalHits`, `HitsPerPage`, and `NextPage`, which is zero after the last
readable page. The Search API serves only the first 1000 hits of a query, so
`Pages` can cover fewer stories than `TotalHits`, which can be an estimate.
Points and comment counts are as indexed and can lag the live site. A
newest-first search can repeat a story on the next page when newer stories
arrive between reads. Pass a hit's `ID` to `GetItem` for its comments.

Use the generated factories in [the daily digest Flow](go/examples/daily-digest/flow/workflow.go)
and [the story search Flow](go/examples/story-search/flow/workflow.go).
`NewProjectConnection` opens the connection from the project configuration that
`projectconfig.LoadFromEnvironment` loads. `New` and `NewConnection` build a
connection for tests. `WithHTTPClient` supplies a transport for tests or
application networking.

## Agent tools

All three Queries are exposed to agent hosts through the `agent` block in
`connector.yaml`; each language package carries its own operations in
`dex-connector.json`. They are public reads, so none asks for approval. Every optional branch except
`invalidResponse` is classified `rejected` because a read changes nothing;
`invalidResponse` is a provider malfunction the model cannot fix, so it stays
unlisted and agent hosts record it as a failure.

From Go v0.24.0, `Connection.AgentTools()` binds the Queries to a connection as
`sdkgo.AgentTool` values, and `ExposedAgentTools()` lists their metadata. From
Go v0.26.0 the list includes `hacker-news-daily_search_stories`. A
`hackernewsmock` connection returns the same tools for host tests.

## Mocks

The generated `hackernewsmock` package supplies a public compiler story for
the feed and item reads. Script another item when the requested ID differs.
The default `SearchStories` answer, `SearchStoriesCompilerStories`, serves
three zero-based pages that follow `NextPage`, with a story repeated across a
page boundary, an Ask HN post without a URL, and a one-point story.
The [real-Dex example test](go/examples/daily-digest/flow/workflow_mock_integration_test.go)
uses the mock connection and checks the five feed inputs and one item read:

```go
mock := hackernewsmock.New(t, ConnectionName)
```

`ListStoryIDsFeedRejected`, `GetItemItemMissing`, `GetItemItemRejected`, and
`SearchStoriesSearchRejected` cover definite provider outcomes. The
[story search mock test](go/examples/story-search/flow/workflow_mock_integration_test.go)
reads every default page with a real Dex Server. The default item represents one fixed
snapshot; the example test scripts a current publication timestamp.

## Execution contract

All three operations are read-only queries. Each Execute attempt makes one HTTP
request. They use async durability and a 30-second Execute timeout. HTTP 408,
429, 5xx, transport failures, and interrupted response bodies retry. The default
policy allows unlimited attempts within 65 minutes, with exponential backoff from
1 second to 30 seconds. `Retry-After` can extend the delay up to one hour, within
the remaining retry budget. Other non-200 responses select `providerRejected`; for `searchStories`, HTTP
404 does too. Redirects are not followed.

Malformed JSON, mismatched item IDs, unknown item types, invalid child IDs, and
oversized bodies select `invalidResponse`, as do search pages that are missing
their paging counts, answer another page, return more hits than requested, or
contain a hit without a positive story ID or creation time. Invalid operation input selects
`defect` before a request is sent. An unwired optional branch fails the Flow.
The connector has no mutation, idempotency key, Trigger, or external write.

| Configuration | Default | Meaning |
| --- | --- | --- |
| `baseUrl` | `https://hacker-news.firebaseio.com/v0` | Feed and item API base URL; HTTPS or loopback HTTP |
| `searchBaseUrl` | `https://hn.algolia.com/api/v1` | Search API base URL for `searchStories`; HTTPS or loopback HTTP |
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

The provider tests cover feed order and limits, item parsing, removal, search
request parameters, endpoint selection, time-window rounding, hit decoding,
plain-text story text and its bound, paging, input validation before sending,
HTTP failure classification, redirects, malformed responses, and size bounds.
The daily digest tests use a real Dex Server for retries, deduplicated reads,
default English, source links, durable waiting, and Worker replacement.
They use deterministic summary output and do not require Codex credentials.
The story search tests use a real Dex Server for a throttled first page,
the fixed search window, paging, repeated stories, the Dex Web views, and
failures before any provider request.
