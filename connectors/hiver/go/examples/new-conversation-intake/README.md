# Hiver new-conversation intake example

This example starts one `HiverNewConversationIntake` Flow for every
conversation that newly appears in the picked Hiver shared inbox, through the
`conversationReceived` Trigger:

1. `RecordReceivedConversation` validates the delivered conversation and
   records it.
2. `ReadReceivedConversation` calls `hiver.NewGetConversationStep`, so the
   record holds the conversation's current status and its message IDs, which
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

Generate strict FDG 2.0 from `connectors/hiver/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/new-conversation-intake/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/new-conversation-intake
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release.

## Configure

Build the release metadata and the Studio bundle as the
[claim-conversation example](../claim-conversation/README.md#configure-and-run)
shows, and start `dexcli dev` with this build directory and the
`--connector-release-override hiver=/tmp/hiver-release` flag. Follow the
[Hiver setup](../../../README.md#hiver-setup), then open **Connections** in Dex
Web and select `hiver / hiver-support`.

- **api_key** is the Hiver admin's API key from **Admin Panel > Integrations >
  Developer APIs** at <https://v2.hiverhq.com/>.
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
`dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/new-conversation-intake
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. `HIVER_LOCAL_API_BASE_URL`
points the connection at a local Hiver stand-in for verification only.

With hosted project storage, each conversation is recorded in the binding's
Trigger inbox before the cursor commits, so a restarted Worker neither misses
nor repeats a conversation. Under `dexcli dev` without project storage, the
cursor stays in process memory, so a restarted local Worker records a new
baseline, and conversations that arrived while it was stopped are not
delivered.

## Test

```bash
GOWORK=off go test ./examples/new-conversation-intake/...
GOWORK=off go test -tags=integration ./examples/new-conversation-intake/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Hiver's conversation list. They check that the baseline starts no
Flow, that each new conversation starts exactly one Flow that reads it back,
and that a conversation that arrives while the Trigger process is stopped is
delivered after a restart whose first request Hiver answers with 429. They also
check that re-pointing the binding at another inbox records a new baseline.
Live Hiver polling has not been verified.
