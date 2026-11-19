# Discord channel announcement

`ChannelAnnouncementFlow` sends a mention-safe message through the static `discord-announcements` connection. It retains the safe input and created message. An unwired provider rejection fails the Flow.

Start `dexcli dev` and set `DEX_FLOW_SERVICE_ADDRESS` to its gRPC address. In Dex Web, configure a Discord bot-token connection named `discord-announcements`. Create the bot token at https://discord.com/developers/applications under your application’s Bot page, invite the bot using OAuth2 > URL Generator with Send Messages permission, and copy a channel ID with Discord Developer Mode. Set `DEX_CONNECTOR_CONFIG_FILE` to the local Dex Web `connections.json` file. Keep the token in the connection, never in the input JSON.

From `connectors/discord/typescript`, run the Worker and starter in separate terminals with the same environment:

```sh
npm run example:announcement
npm run example:announcement:start -- examples/channel-announcement/input.example.json
```

Edit the input file's channel ID before starting. The starter prints the created message. The example source is the runnable application snippet for this package.

To verify with a local provider stand-in and real Dex Worker/Client:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:21110 npm run test:integration
```

The integration fixture binds an available local port automatically. Live Discord authorization, message creation, and reaction behavior remain unverified.

Generate and lint Flow Definition Graph 2.0 with Dex CLI 1.28.0 or newer. From `connectors/discord/typescript`, make the local package visible to the analyzer, then run:

```sh
npm run build
npm install --prefix examples/channel-announcement --no-save --no-package-lock --ignore-scripts .
cd examples/channel-announcement
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```
