# Telegram TypeScript provider operations

`MessageSendFlow` sends one plain text message and persists only the provider's message identity. `ChatLookupFlow` reads and persists chat metadata. Both use the static Dex Web connection name `telegram`.

Install dependencies with `npm ci`, then run `npm run example:worker` after configuring `DEX_FLOW_SERVICE_ADDRESS` and Dex project connection settings. Create the `telegram` connection in Dex Web with a BotFather token. Start the Flows with `SendMessageInput` (`chatId`, `text`) or `GetChatInput` (`chatId`). The bot must be able to access the target chat.

Generate strict FDG 2.0 graphs with:

```bash
dexcli visualize examples/provider-operations/message-send-flow.ts --schema-version 2.0 --json --out /tmp/telegram-typescript-send-fdg --lint app
dexcli visualize examples/provider-operations/chat-lookup-flow.ts --schema-version 2.0 --json --out /tmp/telegram-typescript-chat-fdg --lint app
```

The integration test runs both Flows through a real `dexcli dev` server and manifest mocks. It does not contact Telegram. Live Bot API authorization and delivery remain unverified until a user-owned connection is available.
