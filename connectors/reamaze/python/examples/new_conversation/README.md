# Re:amaze new-conversation example for Python

This example starts one `ReamazeNewConversationPython` Flow for every
conversation created in the watched Re:amaze channel that is not spam, through
the `conversationCreated` Trigger's `new-conversations` binding:

1. `RecordNewConversation` validates the delivered conversation and records it.
2. `ReadNewConversation` calls `new_get_conversation_step`, so the intake
   reflects the conversation's current state and newest messages.
3. `CompleteNewConversation` records an intake summary (slug, channel,
   subject, requester email, status, and message count) and completes.

`accept_customer_conversation` starts no Flow for statuses 3 (Spam) and 9
(Spam identified by AI); the Trigger lists every view, so archived
conversations still start one. The Flow ID is
`reamaze-new-conversation-<Trigger event ID>`, and the event ID is stable per
conversation, so a redelivered conversation finds its existing Flow. The first
start records existing conversations as a baseline and starts no Flow for
them. See the [connector README](../../../README.md#conversationcreated-trigger)
for the cursor, ordering, backlog, and rate-limit rules.

## Configure

Follow the [Re:amaze setup](../../../README.md#reamaze-setup), then create the
`reamaze-brand` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 10
  seconds. Re:amaze limits each API token per minute, shared with operation
  Steps, so keep the default one minute unless Flows must start sooner.
- The `new-conversations` binding's **Watched channel** field saves one
  channel slug, such as `support`, from the `slug` field of
  `GET https://{brand}.reamaze.io/api/v1/channels` or **Settings > Channels**.
  Blank watches every channel of the brand; a changed channel records a new
  baseline.

The Worker reads the saved values once at startup, so restart it after saving.

## Run

From `connectors/reamaze/python`, generate strict FDG 2.0 and start `dexcli dev`
with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/new_conversation/flow.py --schema-version 2.0 --json --out ./build/new_conversation
dexcli dev -flow-rendering-dir ./build
```

Open Dex Web at `http://127.0.0.1:8802`, configure the connection and binding,
set `DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote,
and run the Worker, which also runs the Trigger:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.new_conversation.worker
```

Set `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed. Trigger records, without
payloads or credentials, go to the `dex_connectors.sdk.trigger` logger.

With hosted project storage, each conversation is recorded in the binding's
Trigger inbox before the cursor commits, so a restarted Worker neither misses
nor repeats a conversation. Under `dexcli dev`, the poll cursor stays in
process memory, as in Go, so a restarted local Worker records a new baseline
and conversations created while it was stopped are not delivered.

## Test

```bash
uv run pytest tests/test_new_conversation_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/new_conversation/integration_test.py
```

The integration tests run a real Dex Worker and Client against a fake of
Re:amaze's conversation list and read APIs, with project storage shared by two
Trigger processes. They check that the baseline starts no Flow, that a 429
pauses and resumes the poll, that spam is delivered once and starts no Flow
while open and archived conversations each start one, and that a conversation
created while the Trigger process is stopped arrives after a restart. They
also check that a changed channel records a new baseline without delivering
the gap. Live Re:amaze polling has not been verified.
