# Re:amaze TypeScript connector

`@superdurable/connector-reamaze` supplies all six Re:amaze conversation and contact operations and the `conversationCreated` poll Trigger for Dex TypeScript Flows. It depends on `@superdurable/connectors-sdk` `0.12.0`, which adds the poll Trigger kit. Operation-specific Step factories, models, connection contracts, and mocks are generated from [`connector.yaml`](../connector.yaml) and Go model types. From the repository root, regenerate with `go run ./cmd/connectorctl generate connectors/reamaze/connector.yaml` after changing those sources.

## Install and connect

```sh
npm install @superdurable/connector-reamaze @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.45.4 or later. In Dex Web, create a `reamaze-triage` connection using the brand's `reamaze.io` subdomain, the staff login email, and that staff user's secret API token from **Settings > Developer > API Token**. Re:amaze authenticates each request with HTTP Basic. Store the token only in the private `api_token` credential field, never in Flow input, Attributes, results, Streams, or logs.

At Worker startup, load the project with `loadFromEnvironment()` from `@superdurable/connectors-sdk/projectconfig` and pass the static `ConnectionName` to `newProjectConnection`. The [runnable example](examples/search-conversations/) contains that code. The connection resolves the current credentials before each provider request. `brand`, the optional `maxResponseBytes` safety limit, and `pollInterval` are startup configuration.

## Operations

| Factory | Provider call | Happy branch |
| --- | --- | --- |
| `newSearchConversationsStep` | Search one conversation page | `searched` |
| `newGetConversationStep` | Read a conversation and its newest messages | `found` |
| `newCreateConversationStep` | Create a conversation once | `created` |
| `newUpdateConversationStep` | Set changed status, assignee, and tags | `updated` |
| `newReplyToConversationStep` | Send one reply or internal note | `replied` |
| `newFindContactByEmailStep` | Match contacts by exact email | `found` |

The connector keeps Re:amaze's integer statuses, visibilities, origins, and channel types. Numeric message `origin_id` values retain their exact JSON spelling in the string `originId` output, including values larger than JavaScript's safe integer range. Other declared branches are optional; an unwired optional branch fails the Flow. Reads and `updateConversation` retry transient failures. Create and reply use the SDK's durable single-dispatch fence and read-back key; an unconfirmed write may select `uncertain` instead of sending twice.

## conversationCreated Trigger

`conversationCreated` delivers one `ConversationCreatedEvent` per conversation created in the connection's brand, or in the channel a binding saves, with its first message. Each poll sends `GET /conversations?filter=all`, plus `category=<channel>` for a saved channel, so archived and spam conversations arrive too; an application filters on `conversation.status`. The behavior matches the Go connector, which the [connector README](../README.md#conversationcreated-trigger) describes in full: the first poll reads the newest page and delivers nothing, the cursor keeps the newest creation time, a floor 10 minutes behind it, and at most 800 slug digests seen at or above the floor, the walk detects the listing order and reads at most 16 pages of 30, a larger burst delivers the newest 480 and logs a WARN record, a cursor for another brand or channel or one that does not decode reseeds, and a 429 pauses the connection for `Retry-After`, or one minute without it. The cursor token is byte-compatible with Go, and event IDs and inbox payload values match, so Go and TypeScript replicas can share one binding; `test/conversation_created_go_parity.test.ts` replays a Go recording to prove it. A JavaScript `Date` holds milliseconds, so `occurredAt` drops any sub-millisecond part of a creation time; Re:amaze lists creation times in milliseconds.

The connection's `pollInterval` setting, `pollIntervalMs` on `ReamazeClient`, is the delay between polls; the client rejects one under 10 seconds. Re:amaze's per-minute token limit is shared with operation Steps.

| Function | Use |
| --- | --- |
| `newProjectConversationTriggerRunner(project, connection, routes)` | Durable delivery from a `LoadedProject`: each route's saved channel is loaded (an unsaved binding watches every channel), and each conversation is recorded in the binding's project Trigger inbox before its cursor commits. |
| `newConversationTriggerRunner(connection, routes)` | One poll `Runner` for `ConversationCreatedTriggerRoute` values with your own targets and cursor stores; a route without a cursor keeps it in memory. |
| `newConversationCreatedTrigger({ connection, target })` | One in-memory `TriggerRunner`, like Go's generated `NewConversationCreatedTrigger`; it records a new baseline on every start. |
| `defineConversationCreatedTriggerBinding({ connectionName, bindingName, configurationUI })` | Declares the binding in a Flow's `getConnectorTriggerBindings`, with the `channelSlugEntry` unit (`UIUnitChannelSlugEntry`, port `UIChannelSlugEntryPortChannel`) bound to `/channel`. |

The [`new-conversation` example](examples/new-conversation/README.md) wires a Flow target in [`app.ts`](examples/new-conversation/app.ts):

```ts
const runner = newProjectConversationTriggerRunner(project, connection, [{ bindingName: ConversationCreatedTriggerBinding, target: newConversationTarget(client, flow, options) }]);
```

`runner.run(newTriggerContext(signal))` replays the inboxes, seeds, and polls until the signal aborts, then rejects with `TriggerDeliveryAbortedError`.

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run `npm run test:integration`. This runs the checked-in search Flow and the new-conversation Flow through a real Dex Worker and Client against local Re:amaze-compatible stand-ins. Shared Go-recorded conformance scenarios compare all six operations' requests, outputs, branches, retries, and mutation keys. Live Re:amaze authorization, polling, and provider calls remain unverified.
