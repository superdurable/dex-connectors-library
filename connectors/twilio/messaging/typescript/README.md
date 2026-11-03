# Twilio Messaging TypeScript connector

`@superdurable/connector-twilio-messaging` provides the `sendMessage` Mutation
and `getMessage` Query for Dex TypeScript applications. The package ships
manifest-generated contracts and Step factories; `src/client.ts` and
`src/operations.ts` implement the Twilio Messages HTTP behavior.

Install with `npm install @superdurable/connector-twilio-messaging@0.1.0`.
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
defaults. Secrets are resolved for every call and never belong in Flow input.

The runnable [text-message-delivery example](examples/text-message-delivery/README.md)
uses `newProjectConnection(project, ConnectionName)` and the generated
`newSendMessageStep` and `newGetMessageStep`. Its static connection name is
`twilio-messaging`.

## Behavior

`sendMessage` sends `To`, an optional body, ordered media URLs, and either
`From` or `MessagingServiceSid`. Twilio has no Messages create idempotency key.
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

Run `npm ci && npm run check` in this directory. The example's Dex Worker and
Client integration check needs `dexcli dev` and `DEX_FLOW_SERVICE_ADDRESS`.
Live Twilio authorization, message creation, and delivery were not verified.
