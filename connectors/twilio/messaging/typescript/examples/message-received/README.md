# Twilio message-received example for TypeScript

This example starts one `TwilioInboundMessageTypeScript` Flow for every
inbound SMS, MMS, or WhatsApp message to the configured Twilio number, through
the `messageReceived` Trigger:

1. `RecordInboundMessage` validates the delivered message and records it,
   including its text.
2. `ReadInboundMessage` calls `newGetMessageStep`, so the record holds the
   message's current status from Twilio.
3. `CompleteInboundMessage` records the message read back, marks a
   STOP-family opt-out keyword (Twilio's default opt-out handling already
   honors it), and completes the Flow.

The Flow ID is `twilio-inbound-message-<Trigger event ID>`, and the Trigger
event ID is `twilio-message-<message SID>`, so a redelivered message finds its
existing Flow. Only the happy-path `found` branch is wired; a rejected or
invalid read fails the Flow. The first start records the account's recent
messages as a baseline and starts no Flow for them.

## Configure

Generate and lint its strict Flow Definition Graph 2.0 from
`connectors/twilio/messaging/typescript`:

```sh
npm run build
npm install --prefix examples/message-received --no-save --no-package-lock --ignore-scripts .
cd examples/message-received
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

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
  Leave it blank to start a Flow for every inbound message of the account.

The Worker reads the saved values once at startup, so restart it after saving.
A changed number records a new baseline. Under `dexcli dev` the poll cursor
stays in process memory, so a restarted local Worker also records a new
baseline; hosted project storage keeps it.

## Run

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:message-received
```

[`app.ts`](app.ts) wires the Trigger to the Flow, and [`main.ts`](main.ts) runs
the Worker and the poll runner until interrupted. Trigger records, without
message bodies, addresses, or credentials, go to the console at `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`; `info` by default).

## Test

`npm test` checks the mapping, opt-out keywords, validation, and static binding
in `test/message_received_workflow.test.ts`. With `dexcli dev` running, the
integration tests drive the example application against a local stand-in for
the Twilio Messages list and a real Dex Worker and Client:

```sh
npm run build
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration
```

They check that the baseline starts no Flow, that each inbound message to the
number starts exactly one Flow, that outbound messages and messages to another
number start none, and that messages received while the application is
stopped arrive after a restart. They also check that a 429 pauses the poll,
that an MMS still in `receiving` waits until `received`, and that a changed
receiving number records a new baseline without delivering the gap. Delivery
from a live Twilio account has not been verified.
