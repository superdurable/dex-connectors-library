# Blog post check Flow (TypeScript)

This Flow, `BlogPostCheck`, is the TypeScript form of the
[Go example](../../../go/examples/blog-post-check/README.md) and uses the same
static `web-feed-public` connection, Step types, and branch wiring: it reads a
blog's advertised feed with a conditional GET, or its sitemap and the newest
post pages when it has none. `notModified` completes an unchanged check, and
`notFound` on a post page skips that post; every other optional branch fails
the Flow.

## Strict FDG 2.0 generation

From `connectors/superdurable/web-feed/typescript`:

```bash
npm ci && npm run build
npm install --prefix examples/blog-post-check --no-save --no-package-lock --ignore-scripts .
cd examples/blog-post-check
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out /tmp/web-feed-ts-fdg --open=false
```

The FDG must report `valid: true` with no diagnostics.

## Run

Build the Go release artifact and start `dexcli dev` with
`--connector-release-override web-feed=<artifact-directory>` and the rendering
directory, as the [Go example](../../../go/examples/blog-post-check/README.md#run-with-dex-web)
shows. Save `web-feed / web-feed-public` in Dex Web **Connections** with every
setting blank. Then, from the package directory, start the Worker with the
command Dex Web shows:

```bash
DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run example:worker
```

In another terminal with the same environment, start one check and print its
report:

```bash
npm run example:start -- '{"siteUrl":"https://go.dev/blog/","maxPosts":3}'
npm run example:start -- '{"siteUrl":"https://www.postgresql.org/","pathPrefix":"/about/news/","maxPosts":3}'
```

The input fields are those of the Go example.

## Live verification

On 2026-11-15 (UTC) the Worker ran against `dexcli` v1.47.3 with a local release
override of v0.1.0 and a connection saved through the Dex Web Connections API.
Checks started with `example:start` completed for `https://go.dev/blog/` and
`https://kubernetes.io/blog/` (discovered feeds), `https://blog.rust-lang.org/feed.xml`
(and `isUnchanged: true` on a second check with its validators),
`https://www.postgresql.org/` with `/about/news/` (no feed; sitemap and page
metadata), and `https://kubernetes.io/sitemap.xml` (a sitemap index). A
loopback feed URL failed on `unreachable` and a missing feed on `notFound`.
