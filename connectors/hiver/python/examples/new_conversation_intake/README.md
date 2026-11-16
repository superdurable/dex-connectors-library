# Hiver new-conversation intake example (Python)

This example starts one `HiverNewConversationIntakePython` Flow for every
conversation that newly appears in the picked Hiver shared inbox, through the
`conversationReceived` Trigger:

1. `RecordReceivedConversation` validates the delivered conversation and
   records it.
2. `ReadReceivedConversation` calls `new_get_conversation_step`, so the record
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

From `connectors/hiver/python`, with Python 3.11 or newer on `PATH`:

```bash
dexcli visualize ./examples/new_conversation_intake/flow.py \
  --schema-version 2.0 --json --out ./build/new-conversation-intake
```

The command reports `valid: true` and declares the `new-support-conversations`
binding. Dex CLI v1.47.3 does not read Python configuration UI objects, so the
definition lists no units; the Go example declares the same `inboxPicker` unit
for Dex Web.

## Configure

Follow the [Hiver setup](../../../README.md#hiver-setup), then open
**Connections** in Dex Web and select `hiver / hiver-support`.

- **api_key** is the Hiver admin's API key from **Admin Panel > Integrations >
  Developer APIs** at <https://v2.hiverhq.com/>. It is secret.
- **pollInterval** is the delay between polls of the shared inbox, at least
  one second. Each poll uses `pollPagesPerPoll` of the account's 5000 daily
  Hiver requests, so leave both blank unless the account has spare requests or
  several bindings share it.
- **Shared inbox** on the `new-support-conversations` binding is the Hiver
  inbox ID, a short string of digits such as `105902`, listed as `id` by
  `GET https://api2.hiverhq.com/v1/inboxes`. It is not secret and cannot be
  blank.

The Worker reads the saved values once at startup, so restart it after saving.
Choosing another inbox records a new baseline.

## Run

The Worker reads the `DEX_PROJECT_*` project configuration, or the
`DEX_CONNECTOR_CONFIG_FILE` connections file that Dex Web writes under
`dexcli dev`:

```bash
uv run python -m examples.new_conversation_intake.main
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. With hosted project storage,
each conversation is recorded in the binding's Trigger inbox before the cursor
commits, so a restarted Worker neither misses nor repeats a conversation.

## Test

```bash
uv run pytest tests/test_new_conversation_intake_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dexcli-port> \
  uv run pytest examples/new_conversation_intake/integration_test.py
```

The integration tests run a real Dex Worker and Client against an in-process
stand-in for Hiver's conversation list. They check that the baseline starts no
Flow, that each new conversation starts exactly one Flow that reads it back,
and that a conversation that arrives while the Trigger process is stopped is
delivered after a restart whose first request Hiver answers with 429. They also
check that re-pointing the binding at another inbox records a new baseline.
Live Hiver polling has not been verified.
