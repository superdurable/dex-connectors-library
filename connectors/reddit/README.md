# Reddit connector

The Go, [Python](python/README.md), and [TypeScript](typescript/README.md) packages implement `getPost` through Reddit's OAuth Data API. It accepts a full `t3_` post ID and returns ID, title, subreddit, relative permalink, and score. The operation uses `GET /api/info` with the `read` scope. Empty listings select the optional `missing` branch; definitive rejections, malformed responses, and invalid input have separate optional branches. HTTP 429, timeouts, server errors, and transport failures retry.

Reddit requires [Data API approval](https://support.reddithelp.com/hc/en-us/articles/16160319875092-Reddit-Data-API-Wiki). After approval, create a web app at [Reddit app preferences](https://www.reddit.com/prefs/apps), register the exact Dex Web redirect URI, and configure its client ID and secret in the `reddit` connection. Enter your Reddit username without `/u/`. Dex requests permanent consent for `read`, refreshes OAuth tokens, and sends Reddit's required app and username User-Agent. The remaining URL and request limit fields have safe defaults in Dex Web.

The result is public post metadata, but applications storing titles or other user content must honor Reddit's deletion and retention requirements. The [Go example](go/examples/post/README.md), [Python example](python/examples/post/README.md), and [TypeScript example](typescript/examples/post/README.md) use a static `reddit` connection. The Go-recorded [conformance scenarios](../../conformance/connectors/reddit/scenarios.json) cover all branches and retries. Live Reddit authorization and provider behavior have not been verified in this checkout.

The Python and TypeScript packages offer `getPost` to Dex agent hosts as a read-only tool.
