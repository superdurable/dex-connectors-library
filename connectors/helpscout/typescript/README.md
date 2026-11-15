# Help Scout TypeScript connector

`@superdurable/connector-helpscout` provides `searchConversations`,
`getConversation`, `replyToConversation`, `updateConversation`, and
`findCustomerByEmail` for Dex TypeScript Flows. The manifest and Go model types
generate the public models, validators, Step factories, connection bindings,
mock cases, and OAuth client credentials declaration. From 0.2.0 it also
provides the signed `conversationEvent` webhook Trigger at parity with Go.

Use Node.js 22 or 24, `@superdurable/connectors-sdk@0.11.0`, and Dex TypeScript SDK 1.28.0 or later. At Worker startup,
load the Dex project with `loadFromEnvironment()`, then open the static
`ConnectionName` with `newProjectConnection()`, as the
[runnable search example](examples/search-conversations/README.md) shows.
Configure `app_id` and the secret `app_secret` from **Your Profile > My Apps**
at [Help Scout](https://secure.helpscout.net/). Leave `access_token` blank: the
generated refresh declaration obtains and renews it through the client
credentials grant. The optional `webhook_secret` verifies every webhook the
`conversationEvent` Trigger receives.
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

## conversationEvent Trigger

The runnable [new-conversation tagging example](examples/tag-new-conversations/README.md)
starts one Flow per new active conversation, reads it, and tags it `dex-triaged`.

- `newProjectConversationEventEndpointRunner(project, connectionName, routes)`
  opens the connection and every route's binding from the project
  configuration and wraps each target, such as `newDexFlowTriggerTarget`, in
  the binding's durable project inbox. Mount `runner.serveHTTP` at the public
  HTTPS URL of the Help Scout webhook and run `runner.run(context)` for the
  life of the process. It answers `503` until every binding has replayed its
  pending events and receives, so Help Scout retries; `runningSourceCount()`
  serves a readiness check.
- Every delivery must carry `X-HelpScout-Signature`, the base64 HMAC-SHA1 of the
  raw body keyed with `webhook_secret`, compared in constant time. A wrong,
  missing, or malformed signature, a missing `X-HelpScout-Event`, or a body
  that is not a conversation answers `400`; a blank `webhook_secret` answers
  `503`. Bodies above `webhookMaxBodyBytes` answer `413`, other methods `405`.
- Only the ten conversation events of `conversationWebhookEvents()` are
  decoded; any other valid event is acknowledged with `200` and recorded
  nowhere. A binding's `events` and `mailboxId` (from the `mailboxPicker`
  unit) filter what it records; a filtered event is still acknowledged.
- The event ID is `<event>:<conversation id>:<32 hex digits of the body's
  SHA-256>`, because Help Scout sends no delivery ID: a redelivered body keeps
  its ID and starts no second Flow. `occurredAt` is the receipt time.
- The endpoint reads only `webhook_secret` and renews the stored access token
  only when project storage holds it as expired.
- `conversationEventWebhookHandler(connection)` and
  `newConversationEventTrigger(...)` serve the same endpoint without a durable
  inbox; `defineConversationEventTriggerBinding` declares a static binding and
  its configuration units for Dex Web.

Run `npm ci && npm run check` here for package checks. With `dexcli dev`
running, set `DEX_FLOW_SERVICE_ADDRESS` and run `npm run test:integration`.
The integration tests run the checked-in Flows and a fenced note retry through
a real Dex Worker and Client against a local Help Scout stand-in; the Trigger
tests deliver signed, forged, filtered, and redelivered webhooks to the tagging
example and replay a delivery recorded while its target was unavailable. The
Go-recorded conformance scenarios compare requests, outputs, branches,
retries, and dispatch outcomes.
Live Help Scout authorization, webhook delivery, and provider behavior remain unverified.
