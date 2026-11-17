# Telegram message send Flow

These runnable Flows call `sendMessage` and `editMessageText` with the static `telegram` connection. Optional branches, including `uncertain`, are unwired so they fail the Flow. If a send becomes uncertain, inspect the destination chat before considering a new send.

## Strict FDG 2.0 generation

From `connectors/telegram/go`, run:

```bash
dexcli visualize ./examples/send-message/flow/edit_message_flow.go --schema-version 2.0 --lint app --json --out /tmp/telegram-message-edit-fdg
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev -open=false` with `--connector-release-override telegram=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `telegram / telegram` and follow the [BotFather](https://t.me/BotFather) setup guide. Add the bot to the target chat. Leave base URL, timeout, and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/telegram/go`:

```bash
go run ./examples/send-message
```

In Dex Web Start Flow, choose `TelegramMessageSend`, enter a unique Flow ID, and use:

```json
{"chatId":"@meridian_team","text":"Hello from Dex"}
```

Replace the public username with your accessible chat, or use a decimal chat ID. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show the message identity, not its text. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/send-message/flow` while `dexcli dev` runs.

Choose `TelegramEditMessageText` with a unique Flow ID and input `{"chatId":"@meridian_team","messageId":52,"text":"Edited"}` to replace a bot-editable message's plain text. The result contains only the chat and message IDs.
