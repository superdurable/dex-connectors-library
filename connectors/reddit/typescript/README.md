# Reddit connector for TypeScript

This package implements `getPost`, `listSubredditPosts`, and `searchPosts` with the shared [manifest](../connector.yaml) and replays every Go-recorded provider scenario. See the [connector guide](../README.md) for OAuth setup, inputs, output fields, branches, and retry behavior, including the `X-Ratelimit-Reset` delay on a 429 without `Retry-After`. It requires `@superdurable/connectors-sdk` 0.11.0, exports the `mock` subpath for application tests, and exports `./dex-connector.json`, which offers all three operations to Dex agent hosts as read-only tools.

The [post Flow](examples/post/README.md) and the [topic posts Flow](examples/topic-posts/README.md) use the static `reddit` Dex Web connection. The generated mock answers the paginated `listSubredditPosts` and `searchPosts` defaults with an empty page, because TypeScript mock generation does not render manifest `pages` yet; script pages with `respond` as the topic posts integration test does.

Run `npm ci` and `npm run check` from this directory. With `dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` runs both Flows on a real Dex Worker and Client against a local stand-in for the Reddit API. Live Reddit authorization and Dex Web field rendering have not been verified.
