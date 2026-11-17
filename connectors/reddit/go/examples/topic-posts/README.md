# Reddit topic posts Flow (Go)

This runnable Flow collects recent posts about one topic in one subreddit through the static `reddit` Dex Web connection. Start it with a request such as `{"subreddit":"golang","topic":"generics"}`:

1. the start Step validates and stores the request in the `reddit-topic-request` Attribute;
2. `reddit.NewListSubredditPostsStep` reads the subreddit's newest page of 25 posts;
3. the record Step keeps the posts that are not pinned and whose title or self text contains the topic, ignoring case, in the `reddit-topic-recent-mentions` Attribute;
4. `reddit.NewSearchPostsStep` searches only that subreddit for the topic over the past week, newest first; and
5. the result Step combines both lists without pinned posts, keeps each post ID once, orders them newest first, stores the `TopicPostsOutput` in the `reddit-topic-posts` Attribute, and completes with it. `searchAfter` continues the search to older matches.

Set `readCommunity` to `true` in the start request to run `reddit.NewGetSubredditStep`
after the post search. The Flow then adds the community description and subscriber
count to `TopicPostsOutput.community` before completing. The mock-backed Dex test
exercises this path.

The summary and display RPCs show the three Attributes in Dex Web. Optional branches are unwired, so `subredditUnavailable`, for example after a misspelled subreddit, fails the Flow with the connector's safe failure reason instead of reading as no posts.

## Generate the strict Flow Definition

From `connectors/reddit/go`, use the latest stable dexcli:

```bash
dexcli visualize ./examples/topic-posts/flow/workflow.go --schema-version 2.0 --lint app --json --out /tmp/reddit-topic-posts-fdg --open=false
```

The definition must report `valid: true`. Inside this repository it warns `connector_release_required`, because the local module is not a published release.

## Test with Dex

From `connectors/reddit/go`, `GOWORK=off go test ./examples/topic-posts/...` runs the deterministic tests. With `dexcli dev` running, run:

```bash
GOWORK=off go test -tags=integration ./examples/topic-posts/... -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex does not listen on `127.0.0.1:8801`. The [mock-backed test](flow/workflow_mock_integration_test.go) runs the Flow on a real Dex Worker and Client with the `redditmock` defaults, which include a pinned post and a post that both the listing and the search return, and with a scripted `subredditUnavailable` answer. The [fake provider test](flow/fake_reddit_integration_test.go) runs the same Flow through the real Reddit client against a local stand-in for the Reddit API and checks every request it sends.

## Configure and run

Until this connector version is released, build its release metadata from the repository root and start Dex with it:

```bash
mkdir -p /tmp/reddit-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/reddit/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/reddit/go \
  --version v0.3.0 --tag connectors/reddit/go/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/reddit-release/connector-release.json \
  --digest-output /tmp/reddit-release/connector-release.json.sha256
dexcli dev --connector-release-override reddit=/tmp/reddit-release
```

Follow the [connector setup](../../../README.md) to get Reddit Data API approval and a web app. In Dex Web **Connections**, select `reddit / reddit`, follow the authorization guide, and authorize the `read` scope, which covers both listing and search. From `connectors/reddit/go`, start the Worker with the launch command that Dex Web shows, such as `DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json" GOWORK=off go run ./examples/topic-posts`. In Dex Web **Start Flow**, select `RedditTopicPosts` and enter:

```json
{"subreddit": "golang", "topic": "generics"}
```

The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

This Flow ran on a real `dexcli dev` stack loaded with the release override above, started through Dex Web's Start Flow API, and read a local stand-in for the Reddit API. Live Reddit authorization and provider behavior for this example have not been exercised.
