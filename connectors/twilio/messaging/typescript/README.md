# Twilio Messaging TypeScript connector

`@superdurable/connector-twilio-messaging` provides the `sendMessage` Mutation,
the `getMessage` Query, and the `messageReceived` poll Trigger for Dex
TypeScript applications. The package ships
manifest-generated contracts and Step factories; `src/client.ts` and
`src/operations.ts` implement the Twilio Messages HTTP behavior.

Install with `npm install @superdurable/connector-twilio-messaging@0.2.0`.
Use Node.js 22 or 24 and the released Connector SDK version in `package.json`.

## Connection

Create a Twilio account or subaccount in the [Twilio Console](https://console.twilio.com/).
In Dex Web **Connections**, set `accountSid` to the account's `AC` SID from
**Account Dashboard > Account Info**. Choose `auth-token` and save the secret
`auth_token` from that panel, or choose `api-key` and save `api_key_sid` and
secret `api_key_secret` from **Account > API keys & tokens**. The key must
belong to the same account and Region. `defaultSender` can be an E.164 number,
`whatsapp:` number, or Messaging Service `MG` SID; blank requires `sender` on
each request. Leave `endpoint` and `maxResponseBytes` blank for their manifest
defaults. `pollInterval` is the delay between `messageReceived` polls, a Go
duration of at least 5 seconds. Secrets are resolved for every call and never belong in Flow input.
Stored credentials without `auth_method` use `auth-token`; a rejected static
credential requires updating the connection rather than an automatic refresh.

The runnable [text-message-delivery example](examples/text-message-delivery/README.md)
uses `newProjectConnection(project, ConnectionName)` and the generated
`newSendMessageStep` and `newGetMessageStep`. Its static connection name is
`twilio-messaging`.

## messageReceived Trigger

`messageReceived` polls the account's Messages list with the TypeScript poll
Trigger kit (`@superdurable/connectors-sdk/pollingtrigger`, Connector SDK
0.12.0) at behavior parity with the Go connector, and delivers one
`MessageReceivedEvent` per inbound message: the safe message view plus its
text. The binding's saved `to` (an E.164 number or `whatsapp:+E.164` sender;
blank receives every inbound message of the account) is sent as the list's
`To` filter, and outbound messages are never delivered. The first poll records
the last two minutes as a baseline. Later polls relist from the `date_sent`
watermark minus two minutes, skip SIDs already delivered, hold an MMS still in
`receiving` for up to ten minutes, and deliver at most 500 messages per page,
oldest first. A 429 pauses the connection for `Retry-After`; an undecodable
cursor, or one for another account or number, records a new baseline. The
Trigger event ID is `twilio-message-<SID>`. The
[connector README](../README.md#messagereceived-trigger) describes pagination
and window halving.

The cursor token and keyed snapshot match the Go connector byte for byte, so Go
and TypeScript replicas share a binding's cursor, and a message a Go replica
recorded in the inbox replays in TypeScript.
[`examples/message-received/app.ts`](examples/message-received/app.ts) records
each message in the binding's project Trigger inbox before the watermark
commits:

```ts
const runner = newProjectMessageTriggerRunner(project, connection, [{
  bindingName: MessageReceivedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryInboundMessage, resolveFlowId, mapToFlowInput, { logger: options.logger }),
}], options.logger);
```

`newMessageTriggerRunner` takes targets and cursor stores directly, and
`newMessageReceivedTrigger` runs one binding with an in-memory cursor, so every
start records a new baseline. See the
[message-received example](examples/message-received/README.md).

## Behavior

`sendMessage` sends `To`, an optional body, ordered media URLs, and either
`From` or `MessagingServiceSid`. Form values use Go-compatible query escaping,
including `%2A` for `*` and an unescaped `~`. Twilio has no Messages create idempotency key.
The SDK fence records a heartbeat before dispatch and permits a resend only
after proof of no send or Twilio's 429 or 503 response. A valid 2xx Message
selects `accepted`; a conclusive 4xx selects `providerRejected`; an ambiguous
transport or response selects uncertainty. `getMessage` reads a Message SID,
retries transport, 408, 429, and 5xx failures, and selects `found`, `notFound`,
`providerRejected`, or `invalidResponse` for final results. Unwired optional
branches fail the Flow.

Outputs contain only Twilio's safe message metadata. The body, media URLs,
credential values, and provider error prose are excluded. Status and request
count behavior is replayed against Go recordings in
`conformance/connectors/twilio/messaging/scenarios.json`.

Run `npm ci && npm run check` in this directory. The Trigger tests replay the
Go fetcher and runner tests against a local Messages list fake and compare
cursor tokens, snapshot keys, request queries, and event payloads with
Go-printed values. The examples' Dex Worker and Client integration checks need
`dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`. Live Twilio authorization, message
creation, delivery, and Messages listing were not verified.
