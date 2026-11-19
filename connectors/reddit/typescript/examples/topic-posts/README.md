# Reddit topic posts Flow (TypeScript)

This runnable Flow mirrors the [Go topic posts example](../../../go/examples/topic-posts/README.md). Start it with a request such as `{"subreddit":"golang","topic":"generics"}`: it stores the request, reads the subreddit's newest page with `newListSubredditPostsStep`, keeps the posts that are not pinned and mention the topic, searches the subreddit's past week with `newSearchPostsStep`, and completes with both lists combined, each post once, newest first. `searchAfter` continues the search. Optional branches are unwired, so `subredditUnavailable` fails the Flow.

Configure OAuth in the [connector guide](../../../README.md). Start the Worker with the launch command Dex Web shows, such as `DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" npm run example:topic-posts:worker`, then run `npm run example:topic-posts:start -- '{"subreddit":"golang","topic":"generics"}'`. Dex Web Start Flow serves Go Flow definitions only.

For FDG 2.0 generation, link this unreleased package under `node_modules/@superdurable/connector-reddit`, then run `dexcli visualize examples/topic-posts/flow.ts --schema-version 2.0 --lint app --json --out /tmp/reddit-typescript-topic-posts-fdg --open=false` from the TypeScript package directory; it must report `valid: true`.

With `dexcli dev` running, `npm run test:integration` runs this Flow on a real Dex Worker and Client through the real client against a local Reddit stand-in, including a 429 that carries only `X-Ratelimit-Reset`, and with scripted mock pages. Live Reddit authorization and provider behavior have not been verified.
