# Re:amaze TypeScript connector

`@superdurable/connector-reamaze` supplies all six Re:amaze conversation and contact operations for Dex TypeScript Flows. Operation-specific Step factories, models, connection contracts, and mocks are generated from [`connector.yaml`](../connector.yaml) and Go model types. From the repository root, regenerate with `go run ./cmd/connectorctl generate connectors/reamaze/connector.yaml` after changing those sources.

## Install and connect

```sh
npm install @superdurable/connector-reamaze @superdurable/connectors-sdk @superdurable/dex
```

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. In Dex Web, create a `reamaze-triage` connection using the brand's `reamaze.io` subdomain, the staff login email, and that staff user's secret API token from **Settings > Developer > API Token**. Re:amaze authenticates each request with HTTP Basic. Store the token only in the private `api_token` credential field, never in Flow input, Attributes, results, Streams, or logs.

At Worker startup, load the project with `loadFromEnvironment()` from `@superdurable/connectors-sdk/projectconfig` and pass the static `ConnectionName` to `newProjectConnection`. The [runnable example](examples/search-conversations/) contains that code. The connection resolves the current credentials before each provider request. `brand` and the optional `maxResponseBytes` safety limit are startup configuration.

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

## Verify

```sh
npm ci
npm run check
```

With `dexcli dev` running and `DEX_FLOW_SERVICE_ADDRESS` set, run `npm run test:integration`. This runs the checked-in search Flow through a real Dex Worker and Client against a local Re:amaze-compatible HTTP stand-in. Shared Go-recorded conformance scenarios compare all six operations' requests, outputs, branches, retries, and mutation keys. Live Re:amaze authorization and provider calls remain unverified.
