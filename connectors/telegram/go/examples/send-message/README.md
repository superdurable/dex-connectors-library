# Telegram message send Flow

This runnable Flow calls `sendMessage` with the static `telegram` connection and completes with Telegram's message ID. Optional branches, including `uncertain`, are unwired so they fail the Flow. If a send becomes uncertain, inspect the destination chat before considering a new send.

## Strict FDG 2.0 generation

From `connectors/telegram/go`, run:

```bash
mkdir -p build
dexcli visualize ./examples/send-message/flow/workflow.go --schema-version 2.0 --json --out ./build/send-message
```

The command must report `valid: true`. An unpublished connector can report `connector_release_required` until its release artifact exists.

## Run with Dex Web

Create the connector release artifact, pass it to `dexcli dev` with `--connector-release-override telegram=<artifact-directory>`, and use the generated Flow rendering. In Dex Web Connections, select `telegram / telegram` and follow the [BotFather](https://t.me/BotFather) setup guide. Add the bot to the target chat. Leave base URL, timeout, and response limit blank for their defaults.

With `DEX_PROJECT_*` configured, run from `connectors/telegram/go`:

```bash
go run ./examples/send-message
```

In Dex Web Start Flow, choose `TelegramMessageSend`, enter a unique Flow ID, and use:

```json
{"chatId":"@meridian_team","text":"Hello from Dex"}
```

Replace the public username with your accessible chat, or use a decimal chat ID. The pull Worker connects to Dex Server without an inbound listener. Dex Web summary and display show the message identity, not its text. The mock-backed real-Dex integration test runs with `GOWORK=off go test -tags=integration ./examples/send-message/flow` while `dexcli dev` runs.
