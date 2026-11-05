# Help Scout TypeScript connector

`@superdurable/connector-helpscout` provides `searchConversations`,
`getConversation`, `replyToConversation`, `updateConversation`, and
`findCustomerByEmail` for Dex TypeScript Flows. The manifest and Go model types
generate the public models, validators, Step factories, connection bindings,
mock cases, and OAuth client credentials declaration. The webhook Trigger is
available only in Go.

Use Node.js 22 or 24 and Dex TypeScript SDK 1.28.0 or later. At Worker startup,
load the Dex project with `loadFromEnvironment()`, then open the static
`ConnectionName` with `newProjectConnection()`, as the
[runnable search example](examples/search-conversations/README.md) shows.
Configure `app_id` and the secret `app_secret` from **Your Profile > My Apps**
at [Help Scout](https://secure.helpscout.net/). Leave `access_token` blank: the
generated refresh declaration obtains and renews it through the client
credentials grant. The optional `webhook_secret` is used by the Go Trigger.
Store credentials only in the Dex Web Connection, never in Flow input,
Attributes, Results, Streams, or logs.

| Factory | Provider request | Happy branch |
| --- | --- | --- |
| `newSearchConversationsStep` | `GET /v2/conversations` | `searched` |
| `newGetConversationStep` | `GET /v2/conversations/{id}` and its newest threads | `found` |
| `newReplyToConversationStep` | `POST /v2/conversations/{id}/reply` or `/notes` | `replied` |
| `newUpdateConversationStep` | Read, set final values, read back | `updated` |
| `newFindCustomerByEmailStep` | `GET /v3/customers` | `found` |

Other branches are optional. An unwired optional branch fails its Flow.
`replyToConversation` uses sync durability and a durable dispatch fence: an
unconfirmed sent reply is `uncertain`, while a confirmed 429, 503, or 504
permits a retry. `updateConversation` writes final values and can safely
repeat after a lost answer. Both mutations derive their idempotency key from
the Dex Call ID; Help Scout receives no idempotency header.

Run `npm ci && npm run check` here for package checks. With `dexcli dev`
running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`.
The integration tests run the checked-in Flow and a fenced note retry through
a real Dex Worker and Client against a local Help Scout stand-in. The
Go-recorded conformance scenarios compare requests, outputs, branches,
retries, and dispatch outcomes.
Live Help Scout authorization and provider behavior remain unverified.
