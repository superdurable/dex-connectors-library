# Re:amaze new-conversation example

This example starts one `ReamazeNewConversation` Flow for every conversation
created in the watched Re:amaze channel, through the `conversationCreated`
Trigger:

1. `AcceptCustomerConversation` admits every conversation except spam
   (statuses 3 and 9). The Trigger lists every view, so archived
   conversations still arrive.
2. `RecordNewConversation` validates the delivery and records it.
3. `ReadNewConversation` calls `reamaze.NewGetConversationStep`, so the intake
   reflects the conversation's current state and newest messages.
4. `CompleteNewConversation` records the intake summary and completes the Flow.

The Flow ID is `reamaze-new-conversation-<Trigger event ID>`, and the Trigger
event ID is stable per conversation. A redelivered conversation therefore
finds its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A missing, rejected, or invalid read fails the Flow.

The first start records the channel's existing conversations as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#conversationcreated-trigger) for the
cursor, late-listed conversations, the page bound, and rate limits.

## Configure

Follow the [Re:amaze setup](../../../README.md#reamaze-setup), then configure
the `reamaze-brand` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 10
  seconds. Each poll normally sends one request, and Re:amaze's per-minute
  limit per API token is shared with operation Steps.
- **Watched channel** on the `new-conversations` Trigger binding is the slug
  of one Re:amaze channel, such as `support`, from the `slug` field of
  `GET https://{brand}.reamaze.io/api/v1/channels` or **Settings > Channels**.
  Leave it blank to watch every channel of the brand.

The Worker reads the saved values once at startup, so restart it after saving.
A changed channel records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/reamaze/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/new-conversation/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/new-conversation
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/new-conversation
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. `REAMAZE_LOCAL_API_BASE_URL`
points the Worker at a local Re:amaze stand-in for verification only.

With hosted project storage, each conversation is recorded in the binding's
Trigger inbox before the cursor commits, so a restarted Worker neither misses
nor repeats a conversation. Under `dexcli dev`, the poll cursor stays in
process memory, so a restarted local Worker records a new baseline.
Conversations created while it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/new-conversation/...
GOWORK=off go test -tags=integration ./examples/new-conversation/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Re:amaze's conversation API. They check that the baseline starts
no Flow, that each new conversation is delivered once and starts one Flow
unless it is spam, that a 429 pauses and resumes the poll, and that
conversations created while the Trigger process is stopped arrive after a
restart. They also check that a changed channel records a new baseline without
delivering the gap. Live Re:amaze polling has not been verified.
