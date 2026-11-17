# Telegram chat lookup Flow

These runnable Flows call `getChat` and `setChatTitle` with the static `telegram` connection. Optional branches are unwired, so provider refusals fail the Flow.

## Strict FDG 2.0 generation

From `connectors/telegram/go`, run:

```bash
dexcli visualize ./examples/chat-lookup/flow/set_chat_title_flow.go --schema-version 2.0 --lint app --json --out /tmp/telegram-chat-title-fdg
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev -open=false` with `--connector-release-override telegram=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `telegram / telegram`. Open [BotFather](https://t.me/BotFather), create a bot with `/newbot` or choose one under `/mybots`, and save its token in `bot_token`. Add the bot to the target chat. Grant administrator rights to change the title before starting `TelegramSetChatTitle`. Leave base URL, timeout, and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/telegram/go`:

```bash
go run ./examples/chat-lookup
```

In Dex Web Start Flow, choose `TelegramChatLookup`, enter a unique Flow ID, and use:

```json
{"chatId":"@meridian_team"}
```

Replace the public username with your accessible chat, or use a decimal chat ID. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show the persisted metadata. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/chat-lookup/flow` while `dexcli dev` runs.

Choose `TelegramSetChatTitle` with a unique Flow ID and input `{"chatId":"@meridian_team","title":"Meridian Team"}` to replace a chat title. Telegram returns only a confirmation; the Flow persists the requested chat ID without including the title in Results.
