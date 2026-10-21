# Discord channel-announcement example

This example runs one operation-only Flow, `DiscordChannelAnnouncement`, from
Dex Web **Start Flow** and uses every Discord operation:

1. `RecordAnnouncementRequest` validates the typed start input, applies the
   default `📣` reaction, and persists the request in the
   `discord-announcement-request` Attribute.
2. `GetAnnouncementChannel` calls `discord.NewGetChannelStep` to confirm the
   bot can view the channel, and `AnnouncementChannelFound` records it.
3. `PostAnnouncementMessage` calls `discord.NewCreateMessageStep`. Its nonce
   makes a retried post return the first message instead of a duplicate, and
   `AnnouncementPosted` records the message.
4. `ReactToAnnouncement` calls `discord.NewAddReactionStep`.
5. `ReadBackAnnouncementChannel` calls `discord.NewListChannelMessagesStep` for
   the 10 newest messages, and `CompleteAnnouncement` completes with
   `isConfirmed` set when the posted message is among them.

Only happy-path branches are wired. An unknown channel, a missing permission,
an invalid response, and a local defect fail the Flow. The summary and display
RPCs show the request and the `discord-announcement-outcome` Attribute.

## Validate the Flow Definition

From `connectors/discord`, generate strict FDG 2.0 with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/channel-announcement/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/channel-announcement
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required`, because a local module is not a published
release.

## Configure and run

Dex needs release metadata built from this source, passed as an override; the
connector has no Studio bundle, so the metadata needs no UI artifact. From the
repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/discord-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/discord/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/discord \
  --version v0.1.0 --tag connectors/discord/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/discord-release/connector-release.json \
  --digest-output /tmp/discord-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/discord/build" \
  --connector-release-override discord=/tmp/discord-release
```

Open Dex Web **Connections** and select `discord / discord`. Follow the guide:
create an application at
[discord.com/developers/applications](https://discord.com/developers/applications),
reset and copy the bot token into `bot_token`, and add the bot to a server with
View Channels, Read Message History, Send Messages, and Add Reactions. Leave
`endpoint` and `maxResponseBytes` blank and save. The status becomes **Ready**.

In a second terminal, start the Worker from `connectors/discord` with the
project's `DEX_PROJECT_*` environment, which
[`sdkgo/projectconfig`](../../../../sdkgo/projectconfig/README.md#application-loading)
documents:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/discord"
go run ./examples/channel-announcement
```

The Worker listens on `127.0.0.1:8883`. Override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR` when needed. It logs the
connection name, never the token.

In Discord, enable **User Settings > Advanced > Developer Mode**, right-click
the channel, and choose **Copy Channel ID**. In the Run workspace, choose
**Start Flow**, select `DiscordChannelAnnouncement`, choose the Worker at
`127.0.0.1:8883`, enter a Flow ID, and submit:

```json
{
  "channelId": "112233445566778899",
  "text": "Release 1.2 is out.",
  "reactionEmoji": "🎉"
}
```

The run completes with the channel name, the posted message ID, and
`isConfirmed: true`, and the channel shows the message with the bot's
reaction. A channel ID that is not 17 to 20 digits or blank text fails the
Flow before Discord is called.

## Test

From `connectors/discord`:

```bash
GOWORK=off go test -race ./examples/channel-announcement/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/channel-announcement/... -count=1
```

The real Dex suite runs the Flow against a stateful fake Discord that enforces
message nonces. It covers the complete announcement, a post that Discord
applied but answered with 503, which the retry resolves to one message, an
unknown channel, and invalid start input.
