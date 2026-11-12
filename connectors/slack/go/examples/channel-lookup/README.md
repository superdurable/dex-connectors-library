# Slack channel lookup example

This runnable Flow shows the two Slack discovery reads that an application or
agent uses before posting:

1. `slack.NewGetWorkspaceIdentityStep` reads which workspace and bot the static
   `slack-channel-lookup` connection is authorized for;
2. `slack.NewListChannelsStep` reads one page of channels filtered by the
   request, such as `{"nameFilter":"#general","memberOnly":true}`; and
3. the Flow completes with the workspace and the matching channels, including
   each `channelId` that `PostChannelMessage` and `PostThreadReply` accept.

Optional branches are unwired and fail the Flow if selected. The
[mock-backed integration test](flow/workflow_mock_integration_test.go) runs the
Flow on a real Dex Worker and Client with `slackmock` defaults.

## Generate the strict Flow Definition

From `connectors/slack/go`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/channel-lookup/flow/workflow.go --schema-version 2.0 --json --out ./build/channel-lookup
```

The definition must report `valid: true`. The local module can warn that a
connector release is required.

## Test with Dex

With `dexcli dev` running, from `connectors/slack/go`:

```bash
GOWORK=off go test -tags=integration ./examples/channel-lookup/... -count=1 -v
```

Set `DEX_FLOW_SERVICE_ADDRESS` when Dex does not listen on `127.0.0.1:8801`.

## Configure and run

Create and authorize the Slack app as described in the
[connector guide](../../../README.md#slack-app-setup); listing channels uses the
bot scopes `channels:read` and `groups:read`, which the connection already
requests, and reading the identity needs no scope. In Dex Web
**Connections**, select `slack / slack-channel-lookup`, follow the
authorization guide beginning at [https://api.slack.com/apps](https://api.slack.com/apps),
and save the connection. From `connectors/slack/go`, run
`go run ./examples/channel-lookup` with the `DEX_PROJECT_*` environment
described in [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading).
In Dex Web **Start Flow**, select `SlackChannelLookup` and enter a
`ListChannelsInput` JSON request:

```json
{"nameFilter": "#general", "memberOnly": true}
```

A private channel is listed only after the app is added to it. The Worker
accepts `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` overrides.

Live Slack authorization and provider behavior for this example have not been
exercised; the mock-backed integration test runs against a real Dex server.
