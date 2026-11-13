# WeCom group robot message Flow

This runnable Flow calls `sendGroupRobotMessage` with the static `wecom-robot`
connection and completes with the posted message type and content size.
Optional branches, including `uncertain`, are unwired so they fail the Flow. If
a post becomes uncertain, check the group chat before starting a new Flow.

## Strict FDG 2.0 generation

From `connectors/tencent/wecom/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/group-robot-message/flow/workflow.go --schema-version 2.0 --json --out ./build/group-robot-message
```

The command must report `valid: true`. An unpublished connector can report
`connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with
`--connector-release-override wecom=<artifact-directory>`, and use the
generated Flow rendering. In Dex Web Connections, select `wecom / wecom-robot`,
choose the **Group robot webhook** sender, and follow its guide: in the WeCom
app open the internal group chat > 消息推送 (Message Push, formerly 群机器人) >
添加 (Add), then paste only the value after `key=` into `webhook_key`. Leave
base URL, timeout, and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/tencent/wecom/go`:

```bash
go run ./examples/group-robot-message
```

In Dex Web Start Flow, choose `WeComGroupRobotMessage`, enter a unique Flow ID,
and use:

```json
{"messageType":"markdown","content":"**Deploy 42** finished"}
```

For a text post, set `messageType` to `text` and optionally add
`"mentionedUserIds":["@all"]`. The pull Worker connects to Dex Server without
an inbound listener. Dex Web summary and display show the message type and
size, not its content. The real-Dex integration tests run with
`GOWORK=off go test -tags=integration ./examples/group-robot-message/flow`
while `dexcli dev` runs.
