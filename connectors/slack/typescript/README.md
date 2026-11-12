# Slack TypeScript connector

Install `@superdurable/connector-slack@0.5.0` with `@superdurable/connectors-sdk@0.9.0` and `@superdurable/dex` 1.28 or newer. This package implements `listChannels`, `getWorkspaceIdentity`, `listThreadMessages`, `getThreadReply`, `listChannelMessages`, `postChannelMessage`, and `postThreadReply`. Socket Mode Triggers remain Go-only.

Use `newProjectConnection(project, 'slack-thread-reader')` with a Dex project loaded by `loadFromEnvironment`. The project credential source refreshes the OAuth bot and user tokens using generated manifest declarations. Bot credentials send messages, list channels, read channel history, and read the workspace identity; user credentials read thread replies. The package does not place credentials in Flow state.

The runnable [thread page example](examples/list-thread-messages/README.md) and [channel history example](examples/list-channel-messages/README.md) are the minimal consuming applications and show the static connection name, Worker entrypoint, and Flow Definition Graph 2.0 source. The provider replay tests cover each operation against Go-recorded requests and decisions. Live Slack OAuth and provider behavior have not been verified for this package.

Every query checks the HTTP status before decoding the body: HTTP 429 and 5xx return Retry and honor `Retry-After`, even when Slack sends no JSON body. From 0.4.1 this includes `listThreadMessages` and `getThreadReply`. From 0.5.0, `listChannelMessages` reads one newest-first page of a channel's top-level messages from `conversations.history` with the bot token; `pageSize` is 1 through 15 (blank or 0 reads 15).
