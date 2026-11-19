# Telegram Bot Python connector

The Python package implements `sendMessage` and `getChat` for Dex Flows. A
`sendMessage` attempt uses the Dex single-dispatch checkpoint before contacting
Telegram. A definite 429 or 503 permits a later resend. An unknown outcome
resends while possible sends stay below the send cap and selects `uncertain` at
the cap; a definite refusal is never resent. The bot token remains in Dex credentials and the Bot API
request path, outside Flow state and results.

The runnable [message send](examples/message_send/README.md) and
[chat lookup](examples/chat_lookup/README.md) Flows use the static `telegram` connection.
Provider authorization and live message delivery remain unverified.
