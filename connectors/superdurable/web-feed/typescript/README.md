# Web Feeds TypeScript connector

`@superdurable/connector-web-feed` implements all five Go operations for Dex
TypeScript Flows: `readFeed`, `discoverFeeds`, `readSitemap`,
`readPageMetadata`, and `readPage`, with the factories `newReadFeedStep`,
`newDiscoverFeedsStep`, `newReadSitemapStep`, `newReadPageMetadataStep`, and
`newReadPageStep`.
Inputs, outputs, branches, limits, and defaults are those of the
[connector README](../README.md); every operation replays the Go-recorded
provider scenarios in `conformance/connectors/superdurable/web-feed`,
including exact requests, outputs, and retry delays.

```bash
npm install @superdurable/connector-web-feed
```

The connection has no credentials. Open it with `newProjectConnection(project,
'web-feed-public')`, or build a `WebFeedClient` and bind it with
`newConnection`. `@superdurable/connector-web-feed/mock` exports the generated
`Mock` and its named cases for application tests, and
`@superdurable/connector-web-feed/dex-connector.json` carries the manifest
metadata, including the five agent-exposed operations, for agent hosts.

## Network safety

Requests go through the Connector SDK's `sendProviderRequest` with the
`publicHTTPS` address policy: https only, every resolved address checked, the
connection pinned to a checked address, and no redirect replay. As in Go, an
http URL is requested over https, a refused IP literal is never requested, and
the client follows at most five redirects itself, each through the same
checks. `WebFeedClientOptions.send` replaces the transport for tests; a
replacement must enforce `request.addressPolicy`.

The SDK reports a transport failure without its cause, so after a failed
connection the client resolves the host again and, when every address is
public, tries one TLS handshake to tell a missing host or a certificate that
fails verification (`unreachable`) from a transient failure (Retry).

Differences from Go:

- Go asks for gzip and decompresses it, so a site may answer it with a weak
  `ETag`; the TypeScript client asks for an uncompressed response and may
  receive the strong `ETag`. Each validator works with its own client.
- Malformed XML that Go's non-strict decoder rejects partway can fail here at
  the first invalid UTF-8 byte; both select `invalidResponse`.
- `readPage` extracts text from htmlparser2 element events; the Go connector
  applies the same implied-close and void-element rules to its tokens, so
  both return equal text. Go's tokenizer reads `noscript`, `iframe`,
  `noembed`, `noframes`, `xmp`, and `plaintext` content as raw text while
  htmlparser2 parses it as markup, so an unmatched end tag inside such an
  element can end a region differently; the content itself is dropped in both.

## Runnable example

[Blog post check](examples/blog-post-check/README.md) is the TypeScript form of
the Go example and uses all five operations.

## Verification

From this directory:

```bash
npm ci
npm run typecheck && npm run lint && npm test && npm run build
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

`npm test` runs the 69 conformance scenarios and unit tests for gzip limits,
charsets, literal refusals, retries, page prefixes, readable-text extraction
from malformed markup, and the Go-compatible URL and timestamp helpers. The integration tests run the example on real Dex with
the generated mock. Package release is independent of the Go release.
