# Web Feeds Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/superdurable/web-feed/go@v0.1.0`.

> **Verification status: Live.** The checked-in example ran on a real Dex stack
> through Dex Web against public sites. See [Verification](#verification) for
> what is and is not verified.

The Web Feeds connector reads public web content that sites publish for
machines, so a Dex application can watch blogs and news sites for new posts
without credentials:

| Operation | Reads | Happy path |
| --- | --- | --- |
| `readFeed` | one RSS 2.0, RSS 1.0 (RDF), Atom 1.0, or JSON Feed 1.x document, with conditional GET | `read`, or `notModified` for an unchanged feed |
| `discoverFeeds` | one HTML page's `link rel="alternate"` feed links | `discovered` |
| `readSitemap` | one XML sitemap or sitemap index, plain or gzip, with conditional GET | `read`, or `notModified` for an unchanged sitemap |
| `readPageMetadata` | one HTML page's head metadata: title, description, canonical URL, Open Graph, article times, JSON-LD | `read` |

A typical watcher discovers a blog's feed once and polls it with `readFeed`.
A site that publishes no feed, as many sites built with static-site and
JavaScript frameworks do, usually still publishes `sitemap.xml` with
`lastmod`; the watcher polls the sitemap with a path prefix such as `/blog/`
and reads each new page's metadata. The
[runnable example](go/examples/blog-post-check/README.md) does both.

## Connection

The connection has no credentials. Dex Web shows four settings, each with a
safe default, so a connection is ready as soon as it is saved:

| Setting | Default | Meaning |
| --- | --- | --- |
| `timeout` | `15s` | bound on one HTTP request, at least one second; each redirect hop is a separate request, and the 30-second Execute timeout bounds them all |
| `maxDocumentBytes` | `10485760` | largest feed or sitemap after decompression, from 1024 through 268435456; a larger one selects `invalidResponse` |
| `maxPageBytes` | `1048576` | bytes of an HTML page read by `discoverFeeds` and `readPageMetadata`, from 1024 through 67108864; the rest is not downloaded |
| `userAgent` | `Mozilla/5.0 (compatible; DexWebFeed; +https://connectors.superdurable.io)` | `User-Agent` header, printable ASCII, at most 256 characters |

Site operators see the `User-Agent` in their logs. A deployment may name its
own product and contact page there; never put personal data or a secret in
it. The connection name in the example is `web-feed-public`.

## Operations

Every operation is a Query: it changes nothing, so every transient failure
is retried. Each takes an absolute `http` or `https` `url`. The connector
requests an `http` URL over `https` at the same host, like a browser's
HTTPS-only mode, so a plain-http URL that names a port other than 80 is a
`defect`, and a site that serves no TLS is `unreachable`. A fragment is never
sent. Do not pass a URL that embeds a secret, such as a private feed token:
URLs are Flow input and appear in results.

### `readFeed`

Input: `url`, optional `etag` and `lastModified` from a previous read, `limit`
(1 through 200 items, default 50), and `maxSummaryCharacters` (1 through 4000
code points, default 500).

The result `Feed` holds `format` (`rss`, `rdf`, `atom`, or `json`), `title`,
`siteUrl`, `description`, `updatedAt`, `finalUrl`, the response's `etag` and
`lastModified`, `fetchedAt`, `itemCount` for the whole document, `hasMore`, and
`items` in document order. Each `FeedItem` has:

- `id`: the RSS `guid`, Atom `id`, RDF `rdf:about`, or JSON Feed `id`; else the
  item link; else a `sha256:` digest of its title, dates, and summary. An ID
  longer than 2048 bytes is replaced by its `sha256:` digest.
- `title`, `summary`: plain text with HTML removed and entities decoded. The
  summary is the item summary or, when there is none, its content, cut at a
  word boundary and ended with `…`.
- `link`: absolute, resolved against `xml:base` and the feed URL; for Atom the
  `alternate` link, preferring an HTML type.
- `publishedAt`, `updatedAt`: UTC. RFC 3339, W3C dates, RFC 822 and 1123 with
  named zones (GMT, UT, EST, PDT, and others), RFC 850, and ctime spellings are
  read; a value with no offset is UTC; an unreadable date, or one before
  1970-01-02 such as a zero placeholder, is omitted.
- `authors` (RSS `author` and `dc:creator`, Atom `author`, JSON Feed
  `authors`; Atom and JSON Feed items inherit the feed's authors) and
  `categories`, each at most 20 values.

XML in any encoding its declaration or the HTTP `charset` names is decoded.
HTML entities and missing end tags, which many feeds contain, are tolerated;
external entities are never loaded.

Conditional GET: pass the previous `etag` and `lastModified` unchanged. The
connector sends them as `If-None-Match` and `If-Modified-Since`. A `304`
selects `notModified`, whose result holds `finalUrl`, the refreshed
validators, `fetchedAt`, and no items. A poller wires `notModified` and keeps
its previous state.

### `discoverFeeds`

Input: `url` of an HTML page, usually a site's home page or blog index. The
result `DiscoveredFeeds` lists up to 20 `feeds` in page order, each with
`url`, `title`, and `mediaType` (`application/rss+xml`,
`application/atom+xml`, or `application/feed+json`), resolved against the
page's `base` element without a fragment and without duplicates. A link with
`type="application/json"` is ignored, because content management systems
advertise API resources with it. An empty list means the page advertises no
feed. The result also holds `finalUrl`, `pageTitle`, and `isTruncated`. The
connector never guesses feed paths and never requests the feeds.

### `readSitemap`

Input: `url`, optional `pathPrefix` (starts with `/`, compared
case-sensitively with each entry's URL path), `lastModifiedAfter` (RFC 3339),
`limit` (1 through 2000, default 200), and `etag` and `lastModified` from a
previous read.

The result `Sitemap` has `kind` `urlset` or `sitemapIndex`:

- For a `urlset`, `entries` holds the matching pages newest first: dated
  entries by date descending, then undated ones in document order. Each
  `SitemapEntry` has `url`, `lastModifiedAt`, and, from a Google News sitemap,
  `newsTitle` and `newsPublishedAt`. An entry matches `lastModifiedAfter` when
  its `lastmod`, or its news publication date when `lastmod` is missing, is
  strictly later; an entry with neither never matches.
- For a `sitemapIndex`, `sitemaps` holds the child sitemaps ordered the same
  way, unfiltered. Read the children you need with `readSitemap`.

`totalEntryCount` counts the document's entries with a usable URL,
`matchedEntryCount` those that pass the filters, and `hasMore` reports that
the limit cut the list. A body that starts with the gzip magic number is
decompressed within `maxDocumentBytes`. Conditional GET works as for
`readFeed`.

### `readPageMetadata`

Input: `url` of one HTML page. The result `PageMetadata` holds `finalUrl`,
`title`, `description` (`meta name="description"`), `canonicalUrl`,
`openGraph` (`title`, `description`, `imageUrl`, `type`, `siteName`),
`articlePublishedAt` and `articleModifiedAt` (`article:published_time` and
`article:modified_time`), and `structuredData` (`type`, `headline`,
`datePublished`, `dateModified`) from the first JSON-LD node whose type is an
article type, such as `BlogPosting` or `NewsArticle`, searching `@graph`
lists; without one, the first node with a headline or date. `publishedAt`
and `modifiedAt` pick the article properties first, then JSON-LD, then
`og:updated_time` for `modifiedAt`. Only the first `maxPageBytes` are read and
`isTruncated` reports a longer page; head metadata comes first, so a cut page
still yields it. Scripts never run and linked resources are never requested.

### Outcomes

Every operation classifies its outcome the same way:

| Outcome | Selected when |
| --- | --- |
| happy path | a `2xx` response that parses as the expected document |
| `notModified` | `304` to a conditional GET (`readFeed` and `readSitemap`) |
| `notFound` | `404` or `410` |
| `providerRejected` | any other `4xx`, such as `401`, `403` (often bot protection), or `451` |
| `unreachable` | the host does not exist (`NXDOMAIN`), resolves to an address the connector never connects to, presents a certificate that fails verification, or serves no TLS on the port |
| `invalidResponse` | an unexpected media type, an HTML page where a feed or sitemap is expected, malformed XML, JSON, or gzip, a document over `maxDocumentBytes`, `204`, `304` without validators, more than five redirects, or a redirect to a non-http URL |
| `defect` | invalid input, such as a relative URL, user information, a plain-http port, an out-of-range limit, or a validator that is not printable ASCII; nothing was sent |
| Retry | `408`, `425`, `429`, `5xx`, a timeout, a dropped connection, or an interrupted body; `Retry-After` is honored |

Applications wire only the happy path by default; an unwired branch fails
the Flow. Wire another branch only for a business reaction, such as
`notModified` in a poller or `notFound` to skip a removed page. Failure
messages name the status or rule, never the URL, a header, or body text.
Retries follow the default Connector Step policy: one-second initial delay,
coefficient 2, five-minute maximum delay, for up to one hour. A watcher of
many sites that must not fail on one broken site routes `ExecuteFailure` or
shortens the policy with `StepOptionsOverride.ExecuteRetry`.

## Agent tools

All four operations are reviewed for agent use and set `agent.exposed: true`,
so an agent host, such as the Connectors MCP server, offers them as the tools
`web-feed_read_feed`, `web-feed_discover_feeds`, `web-feed_read_sitemap`, and
`web-feed_read_page_metadata`. They are reads, so none recommends approval.
Every optional branch is classified `rejected`: the call changed nothing and
the model can act on the outcome, such as reading a feed found with
`discover_feeds` after `read_feed` reports an HTML page, or reading again
without validators after `notModified`. Go hosts use
`ExposedAgentTools()` and `Connection.AgentTools()`; each tool's input schema
requires only `url`. The address policy applies to agent calls as to Flow
Steps, so a model cannot reach a private network through these tools.

## Security

The connector fetches URLs that applications, and through them possibly end
users, supply, so it guards against server-side request forgery with the
`sdkgo/providerhttp` `PublicHTTPS` address policy:

- Every request uses https. The host is resolved once, every resolved
  address is checked, and the client connects only to a checked address when
  all of them are public. Loopback, private (`10/8`, `172.16/12`,
  `192.168/16`, `fc00::/7`), shared (`100.64/10`), link-local (including the
  cloud metadata addresses `169.254.169.254` and `fd00:ec2::254`),
  unspecified, and multicast addresses are refused before any byte is sent;
  a host written as such an IP literal is refused before a connection is
  even opened.
- Redirects are followed by the connector, at most five hops, and each hop
  goes through the same checks, so a public URL cannot redirect into a
  private network.
- Proxy environment settings are ignored, because a proxy would receive the
  connection instead of the checked address. A Worker that can reach the
  internet only through a proxy cannot use this connector.
- Requests carry no cookies and no credentials. `WithHTTPClient` accepts a
  base client for TLS settings, but its transport must be an `*http.Transport`
  so the policy owns the dialer, and its cookie jar is dropped.
- Responses are read within `maxDocumentBytes` or `maxPageBytes`, gzip bodies
  are bounded after decompression, XML nesting is limited, and every text
  field is bounded. Text in results is untrusted provider text: escape it
  before rendering it as HTML.

Respect each site's terms and `robots.txt`. Poll at a modest interval and use
conditional GET; the connector honors `Retry-After` but keeps no schedule of
its own.

## Runnable example

[Blog post check](go/examples/blog-post-check/README.md) discovers a blog's
feed and reports its newest posts, or, when the blog has no feed, reads its
sitemap and the newest post pages. It uses all four operations through the
static `web-feed-public` connection.

## Verification

From `connectors/superdurable/web-feed/go`:

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
GOWORK=off go vet -tags=integration ./...
GOWORK=off go test -p 1 -tags=integration ./... -count=1
```

The unit tests serve feeds, sitemaps, and pages from a local TLS server whose
host names resolve, through the policy's test hook, to a public documentation
address or to loopback, private, shared, and link-local addresses. They cover
every format and branch, redirects across hosts, `http` upgrades, the
redirect limit, refused addresses at the first hop and after a redirect,
`NXDOMAIN`, a certificate mismatch, gzip limits, charsets, and the timestamp
spellings above. The integration tests need a running `dexcli dev -durability
process-crash-safe` and use `DEX_FLOW_SERVICE_ADDRESS` when it is not
`127.0.0.1:8801`; they run the example on real Dex with the generated
`webfeedmock` connection, including a Dex retry, the `notModified` wiring, a
sitemap index with two children, and a skipped removed page.

Live verification is recorded in the
[example README](go/examples/blog-post-check/README.md#live-verification).
Not verified: plain-http-only sites (refused by design), sites behind bot
protection that answer `403` to non-browser clients, JSON Feed documents with
non-string tags, and polling behavior over hours, such as servers that change
`ETag` on every response.
