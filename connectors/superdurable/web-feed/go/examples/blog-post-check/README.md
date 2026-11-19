# Blog post check Flow

This runnable Flow, `BlogPostCheck`, checks one blog for recent posts with
the static `web-feed-public` connection and completes with a report:

1. With `feedUrl`, it reads that feed. With `siteUrl`, it first runs
   `discoverFeeds` on the page and reads the first advertised feed.
2. `readFeed` sends the optional `etag` and `lastModified` of a previous check.
   An unchanged feed selects `notModified`, which the Flow wires to complete
   with `isUnchanged: true`; otherwise it reports up to `maxPosts` items
   published after `publishedAfter`, in feed order.
3. When the page advertises no feed, or only `sitemapUrl` is given, it reads
   the sitemap, `/sitemap.xml` at the page's origin by default, filtered by
   `pathPrefix` and `publishedAfter`. For a sitemap index it reads up to three
   child sitemaps, newest first. It then runs `readPageMetadata` on the newest
   `maxPosts` entries for their titles, descriptions, and publication times. A
   page that answers `404` or `410` is skipped; the Flow wires `notFound` for
   that reaction only.
4. With `includeFirstPostText`, it runs `readPage` on the first reported post
   and adds up to 2000 code points of its readable text to the report as
   `firstPostText`. Without posts it completes without reading a page.

Every other optional branch is unwired, so it fails the Flow with the
connector's safe failure, such as `unreachable` for a private address. The
report carries the feed validators to pass to the next check. A recurring
watcher would keep them in its own state and start one check per poll.

## Strict FDG 2.0 generation

From `connectors/superdurable/web-feed/go`, run:

```bash
mkdir -p /tmp/web-feed/graphs
dexcli visualize ./examples/blog-post-check/flow/workflow.go --schema-version 2.0 --json --out /tmp/web-feed/graphs/blog-post-check
```

The command must report `valid: true`. An unpublished connector reports
`connector_release_required` warnings until its release exists.

## Run with Dex Web

From the repository root, build the release artifact for an unreleased
checkout. This publishes nothing:

```bash
mkdir -p /tmp/web-feed/release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/superdurable/web-feed/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go \
  --version v0.1.0 --tag connectors/superdurable/web-feed/go/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/web-feed/release/connector-release.json \
  --digest-output /tmp/web-feed/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/web-feed/graphs \
  --connector-release-override web-feed=/tmp/web-feed/release
```

In Dex Web **Connections**, select `web-feed / web-feed-public` and save it
with every setting blank; it needs no credentials and shows **Ready** and
**Local override**. Start the Worker from `connectors/superdurable/web-feed/go`
with the launch command Dex Web shows:

```bash
DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/blog-post-check
```

In **Start Flow**, choose `BlogPostCheck`, enter a unique Flow ID, and use one
of these inputs:

```json
{"siteUrl": "https://go.dev/blog/", "maxPosts": 3}
```

```json
{"siteUrl": "https://www.postgresql.org/", "pathPrefix": "/about/news/", "maxPosts": 3}
```

```json
{"sitemapUrl": "https://kubernetes.io/sitemap.xml", "pathPrefix": "/blog/", "publishedAfter": "2026-10-15T00:00:00Z", "maxPosts": 2}
```

To read a post's text, add `"includeFirstPostText": true` to any input. To see a conditional GET, run `{"feedUrl": "https://blog.rust-lang.org/feed.xml"}`,
then start another check that adds the reported `etag` and `lastModified`.

| Field | Default | Meaning |
| --- | --- | --- |
| `siteUrl` | none | blog home or index page whose feeds are discovered |
| `feedUrl` | none | feed to read without discovery |
| `sitemapUrl` | `/sitemap.xml` at the page origin | sitemap read when no feed is found |
| `pathPrefix` | every path | sitemap path filter, such as `/blog/` |
| `publishedAfter` | none | RFC 3339 time; only newer posts are reported |
| `maxPosts` | `5` | reported posts, from 1 through 10 |
| `etag`, `lastModified` | none | feed validators from a previous check |
| `includeFirstPostText` | `false` | read the first reported post's text into `firstPostText` |

Give at least one of `siteUrl`, `feedUrl`, or `sitemapUrl`. Dex Web summary
and display show the stored report. The real-Dex integration tests run with
`GOWORK=off go test -tags=integration ./examples/blog-post-check/flow` while
`dexcli dev` runs; they use the generated `webfeedmock` connection.

## Live verification

On 2026-11-15 (UTC) the Flow ran on `dexcli` v1.47.3 with a local release override
of v0.1.0. The connection was saved through the Dex Web Connections API with
every setting blank and showed **Ready** and **Local override**; each run was
started through the Dex Web Start Flow API and read through Dex Web display:

| Input | Path | Result |
| --- | --- | --- |
| `siteUrl` `https://go.dev/blog/` | discovered the Atom feed | completed with three posts |
| `siteUrl` `https://kubernetes.io/blog/` with `publishedAfter` | discovered the RSS feed | completed with the two newer posts |
| `feedUrl` `https://blog.rust-lang.org/feed.xml` | Atom feed | completed with two posts, a weak `ETag`, and `Last-Modified` |
| the same feed with those validators | `304` | `notModified`, completed with `isUnchanged: true` |
| `feedUrl` `https://kubernetes.io/feed.xml` with its `ETag` | `304` | `notModified`, completed with `isUnchanged: true` |
| `siteUrl` `https://www.postgresql.org/` with `pathPrefix` `/about/news/` | no feed; a urlset of about 30,000 entries without `lastmod` | completed with three posts whose titles and dates came from `readPageMetadata` |
| `sitemapUrl` `https://kubernetes.io/sitemap.xml` with `pathPrefix` and `publishedAfter` | sitemap index, three child sitemaps | completed with the two newer posts |
| `feedUrl` `https://go.dev/blog/no-such-feed.xml` | `404` | failed on the unwired `notFound` branch |
| `feedUrl` `https://localhost/feed.xml` | loopback address | failed on the unwired `unreachable` branch; nothing was sent |

Outside the Flow, the same operations read a JSON Feed
(`https://www.jsonfeed.org/feed.json`), a Google News sitemap with
`news:title` and `news:publication_date`, a JSON-LD `LiveBlogPosting`, and
refused `169.254.169.254` before connecting.

Later on 2026-11-15, `readPage` was added. Outside the Flow it read the
text of public posts on go.dev, blog.rust-lang.org, kubernetes.io, and
postgresql.org, a Wikipedia article, and a plain-text RFC. The
`includeFirstPostText` path ran on real Dex with the `webfeedmock`
connection only; it has not yet run through Dex Web against a public site.
