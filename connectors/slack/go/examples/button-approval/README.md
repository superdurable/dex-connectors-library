# Slack button approval example

This runnable Flow replaces keyword replies with Approve and Reject buttons:

1. a matching top-level channel message starts a Flow through the
   `channelThreadCreated` Trigger;
2. the Flow replies in that thread with `slack.NewPostButtonMessageStep`, whose
   message carries an `approve` and a `reject` button;
3. a click arrives through the `buttonClicked` Trigger and invokes the typed
   `ReceiveButtonDecision` RPC; and
4. the Flow replies `Approved by @member.` or `Rejected by @member.` with
   `PostThreadReply` and completes.

Both Triggers run on one `NewProjectMessageTriggerRunner`, because Slack sends
each Socket Mode event to only one of an app's open connections. The button
click resolves the same Flow ID as the request, built from the team ID, channel
ID, and the request's thread timestamp. The Flow writes that timestamp into
each button's value, so the click does not depend on Slack reporting the
thread of the clicked message.

The RPC accepts the first Approve or Reject click on the button message it
posted, whose timestamp it stored, and stores the click's event ID. A
redelivery of that click is reported as a duplicate, and a later click changes
nothing. A click on another message, even with the same action ID and value,
never decides. The button value is the request's thread timestamp, not a
secret: like message text, it is delivered to the Flow and recorded in the
durable inbox. A click on a completed approval is consumed as undeliverable, so it
cannot stop the shared runner. A click that arrives while the buttons are still
being recorded keeps retrying until the Flow waits for it.

The action IDs `approve` and `reject` are code constants. The application's
`NewButtonTriggerFilter` accepts only those buttons in the configured channel
from the configured deciders, so the button binding exposes no action ID field.
Optional connector branches are unwired and fail the Flow if selected.

## Generate the strict Flow Definition

From `connectors/slack/go`, use the latest stable dexcli:

```bash
mkdir -p build
dexcli visualize ./examples/button-approval/flow/workflow.go --schema-version 2.0 --json --out ./build/button-approval
```

The definition must report `valid: true`. The local module can warn that a
connector release is required.

## Test with Dex

With `dexcli dev` running, from `connectors/slack/go`:

```bash
GOWORK=off go test -tags=integration ./examples/button-approval/... -count=1 -v
```

The integration test runs the Flow on a real Dex Worker and Client and serves a
local Slack stand-in with a real Socket Mode WebSocket. It checks the posted
buttons, an ignored click by a non-decider, the deciding click, a late click on
the completed Flow, and that a click is acknowledged well within three seconds
while its delivery retries because the Worker is stopped. Set
`DEX_FLOW_SERVICE_ADDRESS` when Dex does not listen on `127.0.0.1:8801`.

## Configure and run

1. Create and authorize the Slack app as described in the
   [connector guide](../../../README.md#slack-app-setup), including step 7:
   open [https://api.slack.com/apps](https://api.slack.com/apps), select the
   app, open **Interactivity & Shortcuts**, and turn Interactivity on. Socket
   Mode needs no Request URL, and buttons need no scope beyond `chat:write`.
2. Invite the app to the approval channel.
3. In Dex Web **Connections**, select `slack / slack-button-approval`, follow
   the authorization guide, paste the `xapp-` app-level token in `app_token`,
   and save the connection. Button clicks reach only Socket Mode, so the
   runner returns an error on a connection without an `app_token`.
4. Configure the `slack-button-approval-start` binding:
   - **Approval channel** (required): pick the joined channel whose new root
     messages start an approval. The picker stores its channel ID, such as
     `C0123456789`.
   - **Request message contains**: plain text that the root message must
     contain, such as `request approval`; matching ignores case, and blank
     matches every message.
   - **Members allowed to request**: members whose root messages start an
     approval; empty allows any member.
5. Configure the `slack-button-approval-decision` binding:
   - **Approval channel** (required): pick the same channel. Clicks elsewhere
     are ignored.
   - **Members allowed to decide**: the members whose click decides. A click
     by anyone else is ignored; empty lets any member who can see the message
     decide.
6. From `connectors/slack/go`, run `go run ./examples/button-approval` with the
   `DEX_PROJECT_*` environment described in
   [projectconfig](../../../../../sdkgo/projectconfig/README.md#application-loading).
   The Worker also accepts `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`,
   and `LOG_LEVEL=debug`, which logs every ignored message and click with its
   reason.
7. Post `request approval` in the channel, then click **Approve** or
   **Reject** in the thread reply. Restart the Worker after changing a binding.

Live Slack authorization, Interactivity delivery of `block_actions` payloads,
and Slack's rendering of the posted buttons have not been exercised; the
integration test runs against a real Dex server and a local Slack stand-in.
Its payloads follow Slack's documented `block_actions` fields; the
`container.thread_ts` and `message.thread_ts` fields it also sends are not in
Slack's published samples and are unverified.
