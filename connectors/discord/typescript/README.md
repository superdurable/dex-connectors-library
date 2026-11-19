# Discord TypeScript connector

`@superdurable/connector-discord` provides typed Dex Query Steps for `getChannel` and `listChannelMessages`, and Mutation Steps for `createMessage` and `addReaction`. It implements the Discord connector manifest at TypeScript package version `0.1.0`.

Install the package with `npm install @superdurable/connector-discord`. Import `newGetChannelStep`, `newListChannelMessagesStep`, `newCreateMessageStep`, or `newAddReactionStep`, and bind a `DiscordClient` with `newConnection` or a Dex Web connection with `newProjectConnection`. The [runnable channel announcement Flow](examples/channel-announcement/flow/workflow.ts) uses the static `discord-announcements` connection.

The client reads a bot token from the credential provider before each call. The `createMessage` mutation sends a 25-character SHA-256-derived nonce with `enforce_nonce: true` and disables every mention. Discord deduplicates within its short nonce window, so the generated Step limits retries to five attempts and two minutes. `addReaction` is a repeat-safe PUT. Optional branches are unwired by default and fail the Flow if selected.

`getChannel` and `listChannelMessages` read by 17 to 20 digit channel IDs. The latter reads at most 100 messages per page and defaults to 50. A message can be posted as a reply by supplying `replyToMessageId`. Reaction emoji may be Unicode or Discord's `name:id` custom emoji form. Provider text is untrusted; escape it before rendering HTML. Neither Flow input nor persisted Attributes may contain the bot token.

Run `npm ci`, then `npm run check` from this directory. The conformance test replays Go-recorded requests and outcomes. The integration test requires a running `dexcli dev` server and `DEX_FLOW_SERVICE_ADDRESS`; it uses a local Discord stand-in rather than a live provider. See the [example instructions](examples/channel-announcement/README.md).
