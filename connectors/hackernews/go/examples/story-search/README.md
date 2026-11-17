# Hacker News Story Search

This runnable Dex Flow finds recent Hacker News stories that mention a keyword.
It uses the generated `NewSearchStoriesStep` factory, reads result pages in
turn, keeps each story once, and completes with the matching stories, their
original links, and their HN discussion links. It needs no API key and no
model.

## Run locally

Run these commands from the repository root. Install Dex CLI first, then render
the Flow definition:

```sh
mkdir -p /tmp/hn-search/graphs /tmp/hn-search/release
dexcli visualize connectors/hackernews/go/examples/story-search/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/hn-search/graphs/story-search
```

For an unreleased checkout, build a local release override. This does not
publish a release or modify the Go module cache:

```sh
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/hackernews/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/hackernews/go \
  --version v0.27.0 --tag connectors/hackernews/go/v0.27.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/hn-search/release/connector-release.json \
  --digest-output /tmp/hn-search/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/hn-search/graphs \
  --connector-release-override hacker-news-daily=/tmp/hn-search/release
```

Open Dex Web at `http://127.0.0.1:8802`. In **Connections**, save
`hacker-news-public` with no credentials. Leave `baseUrl`, `searchBaseUrl`,
`timeout`, and `maxResponseBytes` blank: the displayed defaults are the public
Hacker News API, the public Hacker News Search API, and safe request limits.
The connection must show **Ready** and **Local override**. In another
terminal, start the Worker from the connector module with the command
that Dex Web shows, which points `DEX_CONNECTOR_CONFIG_FILE` at the saved
connections, as
[local development with Dex Web](../../../../../sdkgo/projectconfig/README.md#local-development-with-dex-web)
describes:

```sh
cd connectors/hackernews/go
DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/story-search
```

In **Start Flow**, choose `StorySearchFlow`. This input finds stories from the
last 24 hours that mention `postgres`:

```json
{"query": "postgres"}
```

This input finds Show HN posts from the last week with at least 5 points:

```json
{"query": "rust compiler", "withinHours": 168, "tags": ["show_hn"], "minPoints": 5}
```

## Input

| Field | Default | Meaning |
| --- | --- | --- |
| `query` | Required | Keyword query, at most 512 bytes; quoted phrases and `-excluded` words follow the Search API syntax |
| `withinHours` | `24` | Keeps stories submitted in the last 1 through 720 hours |
| `tags` | All stories | `show_hn`, `ask_hn`, or `front_page`; a story must carry every listed tag |
| `minPoints` | No minimum | Keeps stories with at least this many points |
| `sortBy` | `date` | `date` reads the newest stories first; `relevance` reads the best matches first |
| `maxPages` | `4` | Reads at most 1 through 20 pages of 50 stories |

## Behavior

The start Step fixes the window when the Flow starts, so every page uses the
same `createdAfter` bound. Each page is one `searchStories` Query Step with the
connector's bounded retries. A newest-first page can repeat a story that a
newer submission pushed down; the Flow keeps the first copy. It stops when the
connector reports no next page or when `maxPages` pages were read, and then
`hasUnreadPages` is true. The Search API serves only the first 1000 hits of a
query, so narrow the window or the query when `totalHits` exceeds that.

The completion result and the `GetDexSummary` and `GetDexDisplay` views hold
the same report: the resolved request, the window start, the stories, the
total hit count, and the pages read. Story text is plain text of at most 2000
characters; titles and text are untrusted provider content.

A blank query, an out-of-range `withinHours`, or an out-of-range `maxPages`
fails the Flow before any provider request. Other invalid input, such as an
unknown tag or negative `minPoints`, selects the connector's `defect` branch.
The example wires only the happy-path `searched` branch, so `defect`,
`providerRejected`, and `invalidResponse` fail the Flow.

## Test

Start a separate Dex development stack, then run the repository targets from
the repository root. The tests use a fake Search API and the generated mock,
so they make no live provider requests:

```sh
make test-connectors
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 make test-connector-integration
```

A live acceptance run uses the same **Connections**, Worker health, and
**Start Flow** endpoints as Dex Web. Inspect the completed report's stories,
window, and links before publishing a change.
