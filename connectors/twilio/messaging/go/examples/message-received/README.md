# Twilio message-received example

This example starts one `TwilioInboundMessage` Flow for every inbound SMS,
MMS, or WhatsApp message to the configured Twilio number, through the
`messageReceived` Trigger:

1. `RecordInboundMessage` validates the delivered message and records it,
   including its text.
2. `ReadInboundMessage` calls `messaging.NewGetMessageStep`, so the record
   holds the message's current status from Twilio.
3. `CompleteInboundMessage` records the message read back, marks a
   STOP-family opt-out keyword (Twilio's default opt-out handling already
   honors it), and completes the Flow.

The Flow ID is `twilio-inbound-message-<Trigger event ID>`, and the Trigger
event ID is `twilio-message-<message SID>`. A redelivered message therefore
finds its existing Flow instead of starting a second one. Only the happy-path
`found` branch is wired. A rejected or invalid read fails the Flow.

The first start records the account's recent messages as a baseline and starts
no Flow for them. See the
[connector README](../../../README.md#messagereceived-trigger) for the
watermark, held MMS, pagination, and rate limits.

## Configure

Follow the [Twilio setup](../../../README.md#twilio-setup), then configure the
`twilio-messaging` connection under **Connections** in Dex Web. Both
authentication methods can read messages; a Restricted API key needs read
access to Messages.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Shorter intervals deliver messages sooner and send more list
  requests against the account's API concurrency limit.
- **Receiving number** on the `inbound-messages` Trigger binding is the Twilio
  number, such as `+14155550100`, or WhatsApp sender, such as
  `whatsapp:+14155550100`, from
  [Phone Numbers > Manage > Active numbers](https://console.twilio.com/us1/develop/phone-numbers/manage/incoming).
  It is typed, not picked, because Studio setup commands cannot send Twilio's
  HTTP Basic credential. Leave it blank to start a Flow for every inbound
  message of the account.

The Worker reads the saved values once at startup, so restart it after saving.
A changed number records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/twilio/messaging/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/message-received/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/message-received
```

The graph is valid; before the connector release it reports only the expected
`connector_release_required` and `connector_trigger_release_required`
warnings.

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/message-received
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each message is recorded in the binding's Trigger
inbox before the watermark commits, so a restarted Worker neither misses nor
repeats a message. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Messages received while it
was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/message-received/...
GOWORK=off go test -tags=integration ./examples/message-received/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for the Twilio Messages list. They check that the baseline starts no
Flow, that each inbound message to the number starts exactly one Flow, that
outbound messages and messages to another number start none, and that
messages received while the Trigger process is stopped arrive after a restart.
They also check that a 429 pauses the poll, that an MMS still in `receiving`
waits until `received`, and that a changed receiving number records a new
baseline without delivering the gap. Live Twilio listing has not been verified.
