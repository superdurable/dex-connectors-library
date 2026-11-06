# Intercom TypeScript connector

Install `@superdurable/connector-intercom@0.1.1` and declare the static
Intercom connection used by the application in Dex Web Connections. The
package implements `searchConversations`, `getConversation`,
`replyToConversation`, `updateConversationState`, and `findContactByEmail`.
The `conversationEvent` webhook Trigger is Go-only.

Import `IntercomClient`, `newProjectConnection`, and the operation-specific
`new*Step` factories from the package root. The checked-in
[read-conversation application](examples/read-conversation) uses
`newGetConversationStep` with a static `ConnectionName` and loads credentials
through `loadFromEnvironment()`. A connection stores its API token privately
in `access_token`; the package resolves it before every provider request.
The optional `client_secret` is only for the Go webhook Trigger.

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

The Go-recorded fixture in `conformance/connectors/intercom/scenarios.json`
checks provider requests, outputs, branches, retries, and uncertainty across
all five TypeScript operations. Live Intercom authorization and provider
behavior remain unverified for this package.
