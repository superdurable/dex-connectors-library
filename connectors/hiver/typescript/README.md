# Hiver connector for Dex TypeScript

`@superdurable/connector-hiver` 0.2.0 implements all six Hiver operations: `listInboxes`, `listConversations`, `getConversation`, `updateConversation`, `addNote`, and `createSharedDraft`, and the `conversationReceived` poll Trigger. It requires `@superdurable/connectors-sdk` 0.12.0. It uses the generated operation-specific Step factories and one static Dex connection. `updateConversation` applies absolute status, assignee, and tag changes and reads them back. Notes and shared drafts use the SDK single-dispatch fence, so an ambiguous write selects `uncertain` without sending it again.

Connection values of zero for `maxResponseBytes`, `requestIntervalMilliseconds`, `pollIntervalMs`, and `pollPagesPerPoll` use the connector defaults, matching the Go package. `HiverClient` rejects a poll interval under one second and a page count outside 2 to 5, as Go's `New` does; `newProjectConnection` reads the stored `pollInterval` as Go duration nanoseconds.

## conversationReceived Trigger

Hiver documents no webhooks, so `conversationReceived` polls one picked shared inbox's conversation list at behavior parity with the Go connector. A binding's first polls walk the whole list, 10 pages of 100 per fetch and continuing at once, and record every conversation ID without delivering it. After that baseline, each poll reads the list's first page plus up to `pollPagesPerPoll` minus one pages of a sweep that walks the rest of the list across polls, and delivers each untracked ID in list order before the cursor commits. IDs that two complete sweeps in a row never list stop being tracked. A rejected page token (HTTP 400, 404, 410, or 422) restarts the walk from the first page and keeps every tracked ID. HTTP 429 pauses every binding of the connection for at least its Retry-After; other failures back off up to 30 minutes. An inbox listing more than `MaxTrackedConversations` (50,000) pauses its binding. Requests share the client's request spacing with operation Steps. Each event ID is `conversation-<inboxId>-<conversationId>`, and the cursor token, tracked IDs, and their hashes match the Go connector byte for byte, so Go and TypeScript replicas share one poll cursor document; `test/conversation_received_go_parity.test.ts` replays a recording of the Go fetcher.

- `newProjectConversationReceivedTriggerRunner(project, connection, routes)` loads each binding's saved `inboxId`, keeps its cursor in project storage, and records each conversation in the binding's project Trigger inbox before the cursor commits, so a restarted replica neither misses nor replays one. A cursor for another inbox records a new baseline.
- `newConversationReceivedTriggerRunner(connection, routes)` takes routes with their own cursor stores and targets.
- `newConversationReceivedTrigger(config)` keeps the cursor in memory, like Go's generated factory, so each start records a new baseline.
- `defineConversationReceivedTriggerBinding` declares the binding; `UIUnitInboxPicker` and `UIInboxPickerPortInboxID` name the release-owned `inboxPicker` unit.

Live Hiver polling is unverified: the list's order, Hiver's answer to an expired `next_page` token, and whether Retry-After accompanies a 429.

## Runnable example

The checked-in [list-inboxes example](examples/list-inboxes/README.md) is a minimal consuming Flow. Configure the static `hiver-support` connection in Dex Web with the API key and the optional response and request-spacing settings described in the [connector README](../README.md). Start `dexcli dev`, then run `npm run example:list` from this package. Start the Flow using `npm run example:list:start -- examples/list-inboxes/input.example.json`.

The [new-conversation-intake example](examples/new-conversation-intake/README.md) starts one Flow per new conversation in the picked shared inbox and reads it back with `getConversation`; run it with `npm run example:new-conversation-intake`.

For the credential-free real Dex integration tests, run `npm run test:integration` with `DEX_FLOW_SERVICE_ADDRESS` set to the address of a running Dex dev server. They run both example Flows through a real Worker and Client against local Hiver stand-ins.

## Verification

From the repository root, regenerate and check the manifest contracts with:

```sh
GOWORK=off go run ./cmd/connectorctl generate connectors/hiver/connector.yaml
GOWORK=off go run ./cmd/connectorctl generate --check connectors/hiver/connector.yaml
```

Run `npm ci && npm run check` from this package. The TypeScript conformance suite replays the Go recordings in `conformance/connectors/hiver/scenarios.json`, including multipart writes, retry decisions, and uncertain outcomes. Live Hiver authorization and provider behavior remain unverified.
