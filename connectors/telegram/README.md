# Telegram Bot connector

The first Go release reads chat metadata through Telegram Bot API `getChat`. Supply a numeric chat ID or public `@username` accessible to the bot. The operation returns the stable numeric ID, chat type, title, and public username. It never returns a token or private message content.

Create a bot with [BotFather](https://t.me/BotFather) using `/newbot`, or open `/mybots` and choose an existing bot. Store its full `numeric-ID:secret` token in the Dex `bot_token` credential. Add the bot to the target chat. The Bot API requires that token in the request path; the connector does not log that path or include it in Dex results, failures, receipts, or Flow input. Revoke an exposed token in BotFather.

`getChat` selects `found`, `notFound`, `providerRejected`, `invalidResponse`, or `defect`. HTTP 429, 503, other server errors, and transport failures retry under the default one-hour policy. The [chat lookup example](go/examples/chat-lookup/README.md) uses the operation-specific Step factory and static `telegram` connection. Send-message and receive-message capabilities from the backlog remain future additions; their delivery and duplicate semantics require separate verification before release.
