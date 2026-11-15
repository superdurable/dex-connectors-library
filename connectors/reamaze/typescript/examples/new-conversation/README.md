# Re:amaze new-conversation example for TypeScript

This example starts one `ReamazeNewConversationTypeScript` Flow for every
conversation created in the watched Re:amaze channel that is not spam, through
the `conversationCreated` Trigger's `new-conversations` binding:

1. `RecordNewConversation` validates the delivered conversation and records it.
2. `ReadNewConversation` calls `newGetConversationStep`, so the intake reflects
   the conversation's current state and newest messages.
3. `CompleteNewConversation` records an intake summary (slug, channel,
   subject, requester email, status, and message count) and completes.

`acceptCustomerConversation` starts no Flow for statuses 3 (Spam) and 9 (Spam
identified by AI); the Trigger lists every view, so archived conversations
still start one. The Flow ID is `reamaze-new-conversation-<Trigger event ID>`,
and the event ID is stable per conversation, so a redelivered conversation
finds its existing Flow. The first start records existing conversations as a
baseline and starts no Flow for them. See the
[connector README](../../../README.md#conversationcreated-trigger) for the
cursor, ordering, backlog, and rate-limit rules.

## Generate the Flow Definition

The analyzer resolves `@superdurable/connector-reamaze` from `node_modules`, so
install the published package (or `npm pack` this package and install the
tarball) first. Then, from this directory:

```sh
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ./build/new-conversation.json --open=false
dexcli dev -flow-rendering-dir ./build
```

The command reports `valid: true` with no diagnostics, and the definition
carries the `new-conversations` binding with its `channelSlugEntry` unit.

## Configure

Follow the [Re:amaze setup](../../../README.md#reamaze-setup), then create the
`reamaze-brand` connection under **Connections** in Dex Web.

- **pollInterval** is the delay between polls, at least 10 seconds. Re:amaze
  limits each API token per minute, shared with operation Steps, so keep the
  default one minute unless Flows must start sooner.
- The `new-conversations` binding's **Watched channel** field saves one channel
  slug, such as `support`, from the `slug` field of
  `GET https://{brand}.reamaze.io/api/v1/channels` or **Settings > Channels**.
  Blank watches every channel of the brand; a changed channel records a new
  baseline.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

With `dexcli dev` running, set `DEX_CONNECTOR_CONFIG_FILE` to the
`connections.json` that Dex Web wrote, then from `connectors/reamaze/typescript`:

```sh
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json npm run example:new-conversation
```

Set `LOG_LEVEL` to `debug`, `info`, `warn`, or `error`; Trigger records carry
no payloads or credentials. `REAMAZE_LOCAL_API_BASE_URL` points the connection
at a local Re:amaze stand-in for verification only.

With hosted project storage, each conversation is recorded in the binding's
Trigger inbox before the cursor commits, so a restarted Worker neither misses
nor repeats a conversation. Under `dexcli dev`, the poll cursor stays in
process memory, as in Go, so a restarted local Worker records a new baseline
and conversations created while it was stopped are not delivered.

## Test

```sh
npx vitest run test/new_conversation_example.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration/new-conversation.integration.test.ts
```

The integration tests run a real Dex Worker and Client against an in-process
fake of Re:amaze's conversation list and read APIs, with project storage
shared by two Trigger processes. They check that the baseline starts no Flow,
that a 429 pauses and resumes the poll, that spam is delivered once and starts
no Flow while open and archived conversations each start one, and that a
conversation created while the Trigger process is stopped arrives after a
restart. They also check that a changed channel records a new baseline
without delivering the gap. Live Re:amaze polling has not been verified.
