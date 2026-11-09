# Telegram Bot Python connector

The Python package implements `sendMessage` and `getChat` for Dex Flows. A
`sendMessage` attempt uses the Dex single-dispatch checkpoint before contacting
Telegram. A definite 429 or 503 permits a later resend; an ambiguous outcome
selects `uncertain`. The bot token remains in Dex credentials and the Bot API
request path, outside Flow state and results.

The runnable [message send](examples/message_send/README.md) and
[chat lookup](examples/chat_lookup/README.md) Flows use the static `telegram` connection.
Provider authorization and live message delivery remain unverified.
