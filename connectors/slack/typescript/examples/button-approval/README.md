# Slack button approval example

This TypeScript example mirrors the Go `button-approval` example:

1. a matching top-level channel message starts a Flow through the `channelThreadCreated` Trigger;
2. the Flow replies in that thread with `newPostButtonMessageStep`, whose message carries an `approve` and a `reject` button;
3. a click arrives through the `buttonClicked` Trigger and invokes the typed `receiveButtonDecision` RPC; and
4. the Flow replies `Approved by @member.` or `Rejected by @member.` with `postThreadReply` and completes.

`app.ts` runs both bindings on one `newProjectMessageTriggerRunner`, because Slack sends each Socket Mode event to only one of an app's open connections. The click resolves the same Flow ID as the request, built from the team ID, channel ID, and the request's thread timestamp, which the Flow writes into each button's value, so the click does not depend on Slack reporting the thread of the clicked message.

The RPC accepts the first Approve or Reject click on the button message it posted, whose timestamp it stored, and stores the click's event ID. A redelivery of that click is reported as a duplicate, and a later click changes nothing. A click on another message, even with the same action ID and value, never decides. The button value is the request's thread timestamp, not a secret: like message text, it is delivered to the Flow and recorded in the durable inbox. A click on a completed approval is consumed as undeliverable, so it cannot stop the shared runner. A click that arrives while the buttons are still being recorded keeps retrying until the Flow waits for it.

The action IDs `approve` and `reject` are code constants. `newButtonTriggerFilter` accepts only those buttons in the configured channel from the configured deciders, so the button binding exposes no action ID field. Optional connector branches are unwired and fail the Flow if selected. The Flow Definition Graph 2.0 source is [`flow/workflow.ts`](flow/workflow.ts); each application Step type has one `dex:group` and one `dex:explanation` directive, and the approval Attribute is registered in the persistence schema.

## Configure and run

1. Create and authorize the Slack app as described in the [connector guide](../../../README.md#slack-app-setup). Open [https://api.slack.com/apps](https://api.slack.com/apps), select the app, open **Interactivity & Shortcuts**, and turn Interactivity on. Socket Mode needs no Request URL, and buttons need no scope beyond `chat:write`.
2. Invite the app to the approval channel.
3. In Dex Web **Connections**, select `slack / slack-button-approval`, follow the authorization guide, paste the `xapp-` app-level token in `app_token`, and save the connection. Clicks reach only Socket Mode, so the runner rejects a connection without an `app_token`.
4. Configure the `slack-button-approval-start` binding: **Approval channel** (required) picks the joined channel whose new root messages start an approval and stores its channel ID, such as `C0123456789`; **Request message contains** is plain text the root message must contain, such as `request approval`, ignoring case, and blank matches every message; **Members allowed to request** empty allows any member.
5. Configure the `slack-button-approval-decision` binding: **Approval channel** (required) picks the same channel, and clicks elsewhere are ignored; **Members allowed to decide** lists the members whose click decides, and empty lets any member who can see the message decide.
6. Run it with the Dex project configured by Dex Web inside `dexcli dev`:

```sh
npm run build
DEX_CONNECTOR_CONFIG_FILE=<path to connections.json> DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx tsx examples/button-approval/main.ts
```

Post `request approval` in the channel, then click **Approve** or **Reject** in the thread reply. Restart the process after changing a binding. Set `LOG_LEVEL=debug` to see every message and click the Triggers ignore, with its reason.

## Test with Dex

With `dexcli dev` running, `DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration` runs the [integration test](../../test/integration/button-approval-triggers.integration.test.ts) on a real Dex Worker and Client with local stand-ins for Slack's Socket Mode and Web API. Like the Go integration tests, one test checks the posted blocks, a non-decider's click filtered by the connector, a copied button on another message that reaches the RPC without deciding, the deciding click, a late click consumed as undeliverable, and one Socket Mode connection for both bindings. The other stops the Worker and checks that a click and a later root message are each acknowledged within Slack's three seconds while the click's delivery retries from its durable inbox, then that the recorded click completes the Flow and the later root starts its own after a Worker returns.

Live Slack authorization, Interactivity delivery of `block_actions` payloads, and Slack's rendering of the posted buttons have not been exercised. The stand-in payloads follow Slack's documented `block_actions` fields; the `container.thread_ts` and `message.thread_ts` fields they also send are not in Slack's published samples and are unverified.
