# Hiver new-conversation intake example (TypeScript)

This example starts one `HiverNewConversationIntakeTypeScript` Flow for every
conversation that newly appears in the picked Hiver shared inbox, through the
`conversationReceived` Trigger:

1. `RecordReceivedConversation` validates the delivered conversation and
   records it.
2. `ReadReceivedConversation` calls `newGetConversationStep`, so the record
   holds the conversation's current status and its message IDs, which
   `createSharedDraft` accepts.
3. `CompleteConversationIntake` records the conversation read back and
   completes the Flow.

The Flow ID is `hiver-new-conversation-<Trigger event ID>`, and the Trigger
event ID is `conversation-<inboxId>-<conversationId>`. A redelivered
conversation therefore finds its existing Flow instead of starting a second
one. Only the happy-path `found` branch is wired. A conversation Hiver no
longer finds, a rejected read, or an invalid response fails the Flow.

The first start walks the inbox's conversation list and records the existing
conversations as a baseline; it starts no Flow for them. See the
[connector README](../../../README.md#conversationreceived-trigger) for how
conversations are found, the request budget, expired page tokens, and limits.

## Generate the Flow Definition

The analyzer resolves `@superdurable/connector-hiver` from `node_modules`, so
install the published package (or `npm pack` this package and install the
tarball) first. Then, from this directory:

```sh
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ./build/new-conversation-intake.json --open=false
dexcli dev -flow-rendering-dir ./build
```

The command reports `valid: true` with no diagnostics, and the definition
carries the `new-support-conversations` binding with its `inboxPicker` unit.

## Configure

Follow the [Hiver setup](../../../README.md#hiver-setup), then open
**Connections** in Dex Web and select `hiver / hiver-support`.

- **api_key** is the Hiver admin's API key from **Admin Panel > Integrations >
  Developer APIs** at <https://v2.hiverhq.com/>. It is secret.
- **pollInterval** is the delay between polls of the shared inbox, a Go
  duration of at least 1s. Each poll uses `pollPagesPerPoll` of the account's
  5000 daily Hiver requests, so leave both blank unless the account has spare
  requests or several bindings share it.
- **Shared inbox** on the `new-support-conversations` Trigger binding is the
  `inboxPicker` unit. **Load inboxes** lists the account's shared inboxes with
  the saved API key and stores the chosen inbox's ID. Enter the ID instead when
  the list cannot load. It cannot be blank.

The Worker reads the saved values once at startup, so restart it after saving.
Choosing another inbox records a new baseline.

## Run

The Worker reads the `DEX_PROJECT_*` project configuration, or the
`DEX_CONNECTOR_CONFIG_FILE` connections file that Dex Web writes under
`dexcli dev`:

```sh
npm run example:new-conversation-intake
```

Override `DEX_FLOW_SERVICE_ADDRESS` or `LOG_LEVEL` (`debug`, `info`, `warn`, or
`error`) when needed. `HIVER_LOCAL_API_BASE_URL` points the connection at a
local Hiver stand-in for verification only. With hosted project storage, each
conversation is recorded in the binding's Trigger inbox before the cursor
commits, so a restarted Worker neither misses nor repeats a conversation.

## Test

```sh
npm test
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dexcli-port> npm run test:integration
```

`test/new_conversation_intake_example.test.ts` checks the mappers and the
binding's unit. The integration tests run a real Dex Worker and Client against
an in-process stand-in for Hiver's conversation list. They check that the
baseline starts no Flow, that each new conversation starts exactly one Flow
that reads it back, and that a conversation that arrives while the Trigger
process is stopped is delivered after a restart whose first request Hiver
answers with 429. They also check that re-pointing the binding at another inbox
records a new baseline. Live Hiver polling has not been verified.
