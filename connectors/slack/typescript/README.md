# Slack TypeScript connector

Install `@superdurable/connector-slack@0.6.0` with `@superdurable/connectors-sdk@0.10.0` and `@superdurable/dex` 1.28 or newer. This package implements `listChannels`, `getWorkspaceIdentity`, `listThreadMessages`, `getThreadReply`, `listChannelMessages`, `postChannelMessage`, and `postThreadReply`, and the `channelThreadCreated` and `threadReplyCreated` Triggers. `buttonClicked` remains Go-only.

Use `newProjectConnection(project, 'slack-thread-reader')` with a Dex project loaded by `loadFromEnvironment`. The project credential source refreshes the OAuth bot and user tokens using generated manifest declarations. Bot credentials send messages, list channels, read channel history, and read the workspace identity; user credentials read thread replies. The package does not place credentials in Flow state.

The runnable [thread page example](examples/list-thread-messages/README.md) and [channel history example](examples/list-channel-messages/README.md) are the minimal consuming applications and show the static connection name, Worker entrypoint, and Flow Definition Graph 2.0 source. The provider replay tests cover each operation against Go-recorded requests and decisions. Live Slack OAuth and provider behavior have not been verified for this package.

Every query checks the HTTP status before decoding the body: HTTP 429 and 5xx return Retry and honor `Retry-After`, even when Slack sends no JSON body. From 0.4.1 this includes `listThreadMessages` and `getThreadReply`. From 0.5.0, `listChannelMessages` reads one newest-first page of a channel's top-level messages from `conversations.history` with the bot token; `pageSize` is 1 through 15 (blank or 0 reads 15).

## Triggers

From 0.6.0, `channelThreadCreated` receives a matching top-level message and `threadReplyCreated` a matching thread reply, at parity with the Go connector. The runnable [thread approval example](examples/thread-approval/README.md) starts a Flow from a root message, approves it from a reply through a typed RPC, and posts a completion reply.

- `newProjectMessageTriggerRunner(project, connectionName, { channelThreadCreatedRoutes, threadReplyCreatedRoutes })` runs every binding of one connection on one runner, because Slack sends each event to only one of an app's Socket Mode connections. Each route's target receives its events through the binding's durable project inbox. Wire targets with `newDexFlowTriggerTarget` and `newDexRPCTriggerTarget` from `@superdurable/connectors-sdk`.
- The runner reads the connection's credential once. With an `app_token` it uses Socket Mode: it acknowledges each envelope after the matching inboxes recorded it, reconnects with a doubling delay from 1 to 30 seconds, and treats 45 seconds without an envelope or ping as a dead connection. Without one, on a shared connection, it serves the Events API callbacks its host relays: mount `runner.serveHTTP` at `relayedTriggerPath(connectionName)`, which is `/triggers/slack/` and the unpadded base64url connection name. The runner registers that route at the host's `/internal/slack-triggers/register` and verifies each callback's `Dex-Relay-Signature` with the scope token within a five-minute window, without reading a Slack credential.
- A root message is delivered before any reply received after it. Bot messages, edited or other subtype messages, and messages without a user are ignored; ignored messages are logged at DEBUG with their reason and never their text.
- `defineChannelThreadCreatedTriggerBinding` and `defineThreadReplyCreatedTriggerBinding` declare static bindings and their configuration units for Dex Web.

Socket Mode, relay registration, signature checks, ordering, and the real Dex example are tested against local stand-ins for Slack and the host. Live Slack Socket Mode and a live host relay have not been verified for this package.
