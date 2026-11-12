# Slack channel marker search example

This runnable Flow shows how an application reconciles a top-level post whose
outcome is unknown. When `PostChannelMessage` selects `uncertain`, there is no
message timestamp to read back, so the application searches recent channel
history for marker text it placed in its own post before deciding whether to
post again:

1. the start Step validates and stores a request such as
   `{"channelId":"C0123456789","marker":"ref:order-1042","oldestTimestamp":"1710000000.000100"}`;
2. `slack.NewGetWorkspaceIdentityStep` reads the bot user ID and bot ID that
   the static `slack-channel-marker-search` connection posts as;
3. `slack.NewListChannelMessagesStep` reads one newest-first page of the
   channel's top-level messages; and
4. the inspection Step completes with the newest message that the bot posted
   and whose text contains the marker, or reads the next older page through
   `nextCursor` until `maximumPages` (1 through 10; blank or 0 reads 3) pages are
   read.

Only the app's own posts match: a message matches when its `botId` equals the
bot ID or its `userId` equals the bot user ID. A person or another app quoting
the marker therefore cannot make the Flow believe the post exists and skip
posting again. `oldestTimestamp` bounds the search to messages after the first
post attempt. Matching is a case-sensitive substring test, so choose a marker
that the app's other posts do not contain, such as a Flow ID. Each page holds
at most 15 messages, the most Slack returns to a non-Marketplace app, and such
an app may read history only once per minute; Slack's 429 responses retry
after `Retry-After`.

Optional branches are unwired and fail the Flow if selected. The
[mock-backed integration test](flow/workflow_mock_integration_test.go) runs the
Flow on a real Dex Worker and Client with `slackmock` defaults, which place
the bot's post on the second page, and with a scripted page where a person
quotes the marker before the bot's post.

## Generate the strict Flow Definition

From `connectors/slack/go`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/channel-marker-search/flow/workflow.go --schema-version 2.0 --json --out ./build/channel-marker-search
```

The definition must report `valid: true`. The local module can warn that a
connector release is required.

## Test with Dex

With `dexcli dev` running, from `connectors/slack/go`:

```bash
GOWORK=off go test -tags=integration ./examples/channel-marker-search/... -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex does not listen on `127.0.0.1:8801`.

## Configure and run

Create and authorize the Slack app as described in the
[connector guide](../../../README.md#slack-app-setup). Reading channel history
uses the bot token's `channels:history` and `groups:history` scopes, which the
connection already requests, and reading the bot identity needs no scope;
invite the app to the channel first. In Dex Web
**Connections**, select `slack / slack-channel-marker-search`, follow the
authorization guide beginning at [https://api.slack.com/apps](https://api.slack.com/apps),
and save the connection. From `connectors/slack/go`, run
`go run ./examples/channel-marker-search` with the `DEX_PROJECT_*` environment
described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading).
In Dex Web **Start Flow**, select `SlackChannelMarkerSearch` and enter a
request:

```json
{"channelId": "C0123456789", "marker": "ref:order-1042", "maximumPages": 2}
```

The Worker accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR`
overrides.

Live Slack authorization and provider behavior for this example have not been
exercised; the mock-backed integration test runs against a real Dex server.
