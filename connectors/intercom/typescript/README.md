# Intercom TypeScript connector

Install `@superdurable/connector-intercom@0.2.0` with
`@superdurable/connectors-sdk@0.11.0` and `@superdurable/dex` 1.28 or newer,
and declare the static Intercom connection used by the application in Dex Web
Connections. The package implements `searchConversations`, `getConversation`,
`replyToConversation`, `updateConversationState`, and `findContactByEmail`,
and from 0.2.0 the `conversationEvent` webhook Trigger at parity with Go.

Import `IntercomClient`, `newProjectConnection`, and the operation-specific
`new*Step` factories from the package root. The checked-in
[read-conversation application](examples/read-conversation) uses
`newGetConversationStep` with a static `ConnectionName` and loads credentials
through `loadFromEnvironment()`. A connection stores its API token privately
in `access_token`; the package resolves it before every provider request.
The optional `client_secret` verifies the `conversationEvent` webhook.

Every API request uses the configured `us`, `eu`, or `au` region and sends
`Intercom-Version: 2.16`. Replies record a Dex heartbeat checkpoint before
dispatch and reconcile a lost response by reading the conversation. State
updates read before writing and re-read a rejected write. Optional branches
remain unwired unless the application needs a specific business reaction.

From this directory, run `npm ci`, `npm run check`, and, with `dexcli dev`
running, `DEX_FLOW_SERVICE_ADDRESS=<host:port> npm run
integration:read-conversation`. The integration script checks a successful
read, a retried rate limit, and an unwired authorization failure through a
real Dex Worker and Client. `npm run example:read-conversation` starts the
Worker against the configured project connection.

## conversationEvent Trigger

The runnable [inbound-conversation application](examples/inbound-conversation)
starts one Flow per new conversation from a signed webhook notification.

- `newProjectConversationEventEndpointRunner(project, connectionName, routes, options)`
  loads each route's stored binding and wraps its target in the binding's
  durable project inbox. Mount `runner.serveHTTP` at the URL entered in the
  Intercom app's **Configure > Webhooks** page and call `runner.run(context)`
  for the life of the process; `runner.runningSourceCount()` serves a
  readiness check. Wire targets with `newDexFlowTriggerTarget` from
  `@superdurable/connectors-sdk`, whose request ID is the notification ID, so
  a redelivery starts no second Flow.
- The endpoint answers Intercom's HEAD validation with 200. It checks
  `X-Hub-Signature: sha1=<hex HMAC-SHA1 of the body>` under `client_secret`
  and answers 400 to a bad signature or envelope, 413 to a body over
  `webhookMaxBodyBytes`, and 503 while `client_secret` is blank, while no
  binding runs, or when recording fails, so Intercom retries. It answers 200
  only after every accepting binding's inbox recorded the notification, and
  200 without recording to `ping` and every topic outside
  `conversationEventTopics()`.
- Each binding's stored `topics` (from the `conversationTopicPicker` unit)
  selects the accepted topics; an empty list accepts every supported topic.
  An unsupported or repeated topic fails startup.
- `ConversationEvent` carries the notification ID, topic, workspace ID,
  notification time, and the conversation without its parts or message body.
  A notification without `created_at` uses its receipt time.
- `newConversationEventTrigger` and `newConversationEventWebhookHandler`
  build one binding without a durable inbox on a connection's shared endpoint,
  and `defineConversationEventTriggerBinding` declares a static binding and its
  configuration units for Dex Web.

`npm test` covers signature, decoding, topic, size, retry, inbox, and replay
behavior against local HTTP requests; `npm run test:integration` runs the
example against a real Dex Server. Live Intercom webhooks are not verified.

The Go-recorded fixture in `conformance/connectors/intercom/scenarios.json`
checks provider requests, outputs, branches, retries, and uncertainty across
all five TypeScript operations. Live Intercom authorization and provider
behavior remain unverified for this package.
