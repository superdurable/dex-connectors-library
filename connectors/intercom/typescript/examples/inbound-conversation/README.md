# Intercom inbound-conversation example

This TypeScript example starts one `IntercomInboundConversation` Flow per new
Intercom conversation through the `conversationEvent` Trigger:

1. `app.ts` mounts `newProjectConversationEventEndpointRunner` at
   `/webhooks/intercom` and runs it at once, so a notification is recorded in
   the binding's durable project inbox even while Dex is unreachable;
2. the `inbound-conversation` binding accepts the topics its
   `conversationTopicPicker` saved, and `acceptInboundConversation` admits only
   `conversation.user.created`;
3. `newDexFlowTriggerTarget` starts the Flow with ID
   `intercom-inbound-conversation-<conversation ID>` and the notification ID
   as request ID, so a redelivery starts no second Flow;
4. `RecordInboundConversation` stores the notification identity, and
   `ReadInboundConversation` calls `getConversation`;
5. `CompleteInboundConversation` completes with the conversation ID, the
   notification ID, Intercom's state, the starting customer's email, and the
   part count.

No message text enters Flow input or state. Optional `getConversation`
branches stay unwired, so a missing conversation or a rejected read fails the
Flow.

## Run it

Follow the [Intercom setup](../../../README.md#intercom-setup) and configure
connection `intercom-support-inbox` in Dex Web Connections with `region`,
`access_token`, and `client_secret`, the app secret that verifies
`X-Hub-Signature`. Then, for the `inbound-conversation` binding, select
**Topics that start the Flow** (`conversation.user.created`) and save. From
`connectors/intercom/typescript`, with `dexcli dev` running and the
`DEX_PROJECT_*` environment naming that project configuration:

```sh
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run example:inbound-conversation
```

The webhook listens on `INTERCOM_WEBHOOK_BIND_ADDRESS` (default
`127.0.0.1:8852`); `GET /readyz` answers `200` once the binding receives
notifications. Set `LOG_LEVEL=debug` to see every delivery. Expose
`http://127.0.0.1:8852/webhooks/intercom` through an HTTPS tunnel and enter it
in the Intercom app's **Configure > Webhooks** page, where Intercom validates
it with a HEAD request. Subscribe to `conversation.user.created` and save.

- A redelivered notification is answered `200` and starts no second Flow.
- A notification whose `X-Hub-Signature` does not match is answered `400` and
  nothing is recorded.
- Intercom's `ping` and unsupported topics are answered `200` and start
  nothing; another accepted topic is recorded and skipped by the Flow filter.
- A notification acknowledged while Dex was down stays in the inbox and is
  replayed when the application restarts.

## Test

`npm test` runs the deterministic [workflow tests](../../test/inbound_conversation_workflow.test.ts):
identities, the binding and its topic picker, the admission rule, the
mappers, and the outcome. With `dexcli dev` running,

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

runs the [integration test](../../test/integration/inbound-conversation-trigger.integration.test.ts):
this application against a real Dex Server and an in-memory project store,
with a local stand-in for the Intercom REST API that requires the bearer token
and `Intercom-Version: 2.16`. A forged signature is refused, a signed
notification and its redelivery start one Flow that completes, a reply topic
is filtered, and the durable inbox drains. Live Intercom webhooks are not
verified.

## Flow Definition Graph

The Steps declare group `inbound` or `intercom` and one explanation each in
[flow.ts](flow.ts). Check strict FDG 2.0 generation from the package
directory with `dexcli visualize examples/inbound-conversation/flow.ts
--language typescript --schema-version 2.0 --lint app --json --out
/tmp/intercom-inbound-conversation`. Dex CLI 1.47.3 reports
`dynamic_class`, `dynamic_step_target`, and `unknown_step_target` for the
generated factory's Step shape, as for the read-conversation example and the
Slack TypeScript example, and `connector_trigger_binding_invalid` because the
example imports the package source rather than a released package. It writes
partial JSON but cannot certify strict FDG 2.0; the real Dex integration test
covers the Flow, and strict visualization remains unverified.
