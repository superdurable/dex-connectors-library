# Slack thread approval example

This example exercises the released Slack Connector end to end:

1. a matching top-level channel message starts a Flow;
2. the Flow reads the first page of that thread with `ListThreadMessages`;
3. a matching reply invokes the typed `ReceiveThreadReply` RPC; and
4. the Flow posts `Processing complete.` to the same thread with `PostThreadReply`.

The root and reply Triggers use the same application resolver to build a
readable Flow ID from Slack team ID, channel ID, and root timestamp. The RPC is
registered and invoked through the same direct bound `ReceiveThreadReply`
method. An RPC input mapper passes only the event ID and approver user ID. The
application RPC locks its thread state and stores the one accepted reply event
ID, so duplicate delivery cannot schedule another completion reply.

The completion Mutation configures the optional `ResultAttribute`. The Flow
registers that typed Attribute explicitly, and its summary/display RPCs read the
full provider result outside the transition chain for operator inspection. This
intentionally duplicates the durable target input; applications should omit the
Attribute when no external reader needs the raw result.

The example runs both message Trigger bindings through one
`NewLocalMessageTriggerRunner`. Slack distributes events among Socket Mode
connections, so one shared connection ensures the root and reply routes both
observe their matching events. Do not run two copies of this example against
the same Slack app during the test.

Before either Dex call, the application supplies a typed filter. The example
builds those filters from the binding configuration saved by Dex Web and checks
the channel, root-or-reply shape, allowed member, and case-insensitive message
substring again. A filtered event is consumed without resolving a Flow ID or
calling Dex. The pure filter has no error result.

## Release baseline

This walkthrough uses these published releases:

- [Slack Connector v0.8.0](https://github.com/superdurable/dex-connectors-library/releases/tag/connectors%2Fslack%2Fv0.8.0)
- [dexcli v0.13.6](https://github.com/superdurable/dex/releases/tag/cli-v0.13.6)

Install Go 1.24 or newer and curl. You also need permission to create and
install an app in the Slack workspace. A workspace administrator may need to
approve the installation.

On macOS, install dexcli with Homebrew:

```bash
brew install superdurable/tap/dexcli
dexcli version
```

If dexcli is already installed, upgrade it instead:

```bash
brew upgrade superdurable/tap/dexcli
dexcli version
```

The final command must report `dexcli v0.13.6` or newer. For Linux or a manual
macOS installation, download the matching archive from the
[cli-v0.13.6 release](https://github.com/superdurable/dex/releases/tag/cli-v0.13.6).

## 1. Prepare a clean local test project

Create a separate directory that consumes the released Connector. The Flow
source is copied from the immutable v0.8.0 tag so dexcli can analyze it as an
application dependency rather than as part of the Connector module itself.

```bash
mkdir slack-thread-approval-e2e
cd slack-thread-approval-e2e
mkdir -p flow build

curl -fsSL \
  https://raw.githubusercontent.com/superdurable/dex-connectors-library/refs/tags/connectors/slack/v0.8.0/connectors/slack/examples/thread-approval/flow/workflow.go \
  -o flow/workflow.go

go mod init example.com/slack-thread-approval-e2e
go get github.com/superdurable/dex-connectors-library/connectors/slack@v0.8.0
go mod tidy
```

Generate the Flow Definition Graph used by Dex Web:

```bash
dexcli visualize ./flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/slack-thread-approval
```

The command must finish without blocking diagnostics and create
`build/slack-thread-approval.json`.

The required compatibility gate repeats this visualization twice in a clean
consumer module using the dexcli release pinned in `.dex-compat-version`. The
scheduled canary separately tests the latest stable dexcli. The output must be
deterministic and retain both connector Steps, both Trigger bindings, all
branch targets, and the Result Attribute.

## 2. Start Dex and record its addresses

From the test project, start Dex with the generated Flow definition and the
default local Connector store:

```bash
dexcli dev \
  --flow-rendering-dir "$PWD/build" \
  --connector-config-dir "$HOME/.dex/connectors"
```

Keep this terminal running. Record the exact Dex Web URL and Dex Server address
printed by dexcli. With the default ports they are:

```text
Dex Web:    http://127.0.0.1:8802
Dex Server: 127.0.0.1:8801
```

Another local process can force different ports. Always use the addresses that
the current dexcli process prints. Do not switch between `127.0.0.1` and
`localhost`, and do not reuse a browser tab from an older dexcli process.

## 3. Create a Slack app from scratch

Open [Your Apps](https://api.slack.com/apps), then:

1. Choose **Create New App**.
2. Choose **From scratch**.
3. Enter an app name such as `dex-thread-approval-e2e`.
4. Select the workspace used for the test.
5. Choose **Create App**.

Keep the Slack app settings open while completing the following sections.

### Record the OAuth client credentials

Open **Basic Information**. Under **App Credentials**, locate:

- **Client ID**;
- **Client Secret**.

Dex Web requests these values when starting OAuth. They remain in memory only
for the ten-minute OAuth session and are not written to the Connector file. Do
not put either value in source control, screenshots, chat, or shell history.

### Add the OAuth redirect URL

Open **OAuth & Permissions**, find **Redirect URLs**, and add:

```text
http://127.0.0.1:8802/api/v2/connector-oauth/callback
```

Replace `8802` with the current Dex Web port when dexcli printed another port.
The scheme, host, port, and path must exactly match the browser origin used for
Dex Web. Choose **Save URLs**.

### Add bot token scopes

On **OAuth & Permissions**, under **Scopes > Bot Token Scopes**, add all of:

```text
channels:history
groups:history
channels:read
groups:read
users:read
chat:write
```

### Add user token scopes

Under **Scopes > User Token Scopes**, add:

```text
channels:history
groups:history
```

The Connector uses the user token for `conversations.replies`. If these scopes
are missing, OAuth may still create a bot token, but reading a thread will fail.

### Subscribe to message events

Open **Event Subscriptions** and turn on **Enable Events**. Under **Subscribe to
bot events**, add the [public-channel](https://docs.slack.dev/reference/events/message.channels/)
and [private-channel](https://docs.slack.dev/reference/events/message.groups/)
message events:

```text
message.channels
message.groups
```

Socket Mode carries these events, so no public Request URL is required.

### Enable Socket Mode and create the app-level token

Open **Socket Mode** and enable it. Socket Mode receives Events API payloads
without a public Request URL. When Slack asks for an app-level token, or from
**Basic Information > App-Level Tokens**, choose **Generate Token and Scopes**:

1. enter a token name such as `dex-local-e2e`;
2. add the `connections:write` scope;
3. generate the token;
4. copy the resulting `xapp-...` value once.

The [`connections:write`](https://docs.slack.dev/reference/scopes/connections.write/)
scope applies to an app-level token. This is not the bot token or user token.
Enter the generated value only in the `app_token` secret field in Dex Web.

### Install the app

Open **OAuth & Permissions** and choose **Install to Workspace**. Review the bot
and user permissions, then allow the installation. If scopes or event
subscriptions are changed later, choose **Reinstall to Workspace** before
testing again.

## 4. Create the Slack test channel and invite the app

In the Slack client:

1. create or open a channel such as `connector-test`;
2. invite the app with `/invite @dex-thread-approval-e2e`;
3. confirm that the app appears in the channel member list;
4. choose at least one human workspace member who will post the approval reply.

The app must be a member of the selected public or private channel. Direct
messages are not supported by this Connector.

Dex Web normally provides channel and member pickers. For the manual fallback:

- open the channel details and choose **Copy channel ID**;
- open a member profile, open the profile menu, and choose **Copy member ID**.

Use the raw stable IDs, not display names:

- a channel ID normally starts with `C`;
- a member ID starts with `U`;
- a workspace/team ID starts with `T`;
- an ID starting with `D` identifies a direct-message conversation and is not
  a member ID.

The workspace/team ID is read from incoming events and does not need to be
entered in Dex Web.

## 5. Configure the connection in Dex Web

Open the exact Dex Web URL printed by the running dexcli process and select
**Connections**. The generated Flow definition should expose:

- connector: `slack`;
- connection: `slack-workspace`;
- `slack-thread-approval-start` for `channelThreadCreated`;
- `slack-thread-approval-reply` for `threadReplyCreated`.

Select `slack / slack-workspace`. In the host-owned **Slack setup** form:

1. enter the **OAuth client ID** from Slack **Basic Information**;
2. enter the **OAuth client secret**;
3. leave `endpoint`, `maxResponseBytes`, and `maxMessageCharacters` at their
   defaults;
4. enter the `xapp-...` token in the `app_token` field;
5. choose **Authorize**.

Slack will ask the signed-in member to approve both bot and user scopes. After
OAuth, the browser returns to the same Dex Web origin and the connection status
should be **Ready**. Dex stores the OAuth `xoxb-...` bot token and `xoxp-...`
user token automatically. Do not copy those tokens into Trigger settings.

The connection file is a plaintext local-development secret store. Its path is
shown at the top of the Connections page, normally:

```text
$HOME/.dex/connectors/connections.json
```

Never commit or share that file.

## 6. Configure both Trigger bindings

In the embedded Slack setup panel, choose **Expand setup** when a full-screen
configuration view is more comfortable. Then:

1. choose **Load channels**;
2. choose **Load members**;
3. select the same approval channel for both Trigger bindings;
4. optionally set **Start when the top-level message contains**;
5. optionally restrict **Members allowed to start approval**; leaving it empty
   permits any human member;
6. set **Approval reply contains** to `approve` or a more distinctive phrase;
7. select at least one **Member allowed to approve**;
8. choose **Save trigger settings**.

The pickers display names for convenience but save stable IDs. If a picker is
unavailable, enter the copied channel ID and comma-separated member IDs in the
fallback fields.

Matching is a case-insensitive substring check. The default `approve` also
matches `disapprove`. Use a distinctive phrase or change the application filter
when exact matching is required.

## 7. Start the released example Worker

Open a second terminal. Use the connection file and Dex Server address printed
by the current dexcli process:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
export DEX_FLOW_SERVICE_ADDRESS="127.0.0.1:8801"

GOWORK=off go run \
  github.com/superdurable/dex-connectors-library/connectors/slack/examples/thread-approval@v0.8.0
```

Replace `127.0.0.1:8801` when dexcli printed another Dex Server address. The
Worker listens on `127.0.0.1:8813` by default. If that address is occupied, set
another one before starting it:

```bash
export DEX_WORKER_BIND_ADDRESS="127.0.0.1:8913"
```

Keep exactly one example Worker running for this Slack app. Slack distributes
Socket Mode envelopes among active WebSocket connections rather than
broadcasting every event to every connection.

## 8. Run the end-to-end test

1. In the configured Slack channel, post a new top-level message as an allowed
   human member. Do not post it as a reply. The message must satisfy the optional
   root text filter.
2. In Dex Web, open the new Flow. Its readable ID has this shape:

   ```text
   slack-thread-approval-<team-id>-<channel-id>-<root-timestamp>
   ```

3. Confirm that the Flow read the root message and reached
   `waitingForReply`.
4. In Slack, reply inside that same thread as an allowed approver. The reply
   must contain the configured approval text, for example `approve`.
5. Confirm that the app posts exactly one reply:

   ```text
   Processing complete.
   ```

6. Confirm in Dex Web that the Flow is **Completed**. Its display includes the
   application-owned thread state and the optional
   `slack-thread-approval-post-reply-result` provider result.

The RPC accepts only the first valid approval while the Flow is waiting. A
second matching approval must not post another completion reply.

For CLI inspection, substitute the actual Flow ID:

```bash
dexcli flow search --server 127.0.0.1:8801 --all --output table
dexcli flow summary --server 127.0.0.1:8801 <flow-id>
dexcli flow inspect --server 127.0.0.1:8801 <flow-id>
```

## 9. Verify restart behavior

After the Flow completes:

1. stop the example Worker with Control-C;
2. start it again with the same command;
3. wait for the Socket Mode connection to reopen;
4. confirm that the completed Slack thread does not receive another completion
   reply.

The Trigger inbox can replay an unfinished delivery after a crash. Stable event
IDs, the readable Flow ID, the application RPC state transition, and Slack
`client_msg_id` prevent replay from duplicating the completed external action.

## Troubleshooting

### Connections does not show `slack-workspace`

Confirm that `build/slack-thread-approval.json` exists and that dexcli started
with the same directory passed to `--flow-rendering-dir`. Re-run visualization
with dexcli v0.13.6 or newer and resolve every blocking diagnostic.

### Slack reports a redirect URL mismatch

Copy the exact current Dex Web origin into the Slack redirect URL. Preserve
`http`, `127.0.0.1`, the current Web port, and
`/api/v2/connector-oauth/callback`. Save the URL before retrying OAuth.

### Dex Web reports an invalid write origin or CSRF token

This usually means the tab belongs to an older dexcli process or a different
port. Close that tab and open the exact URL printed by the current dexcli
process. Do not change `127.0.0.1` to `localhost`. Use dexcli v0.13.6 or newer.

### The channel or member picker is empty

Confirm that OAuth completed, then choose **Load channels** and **Load members**
again. Invite the app to the channel. For a private channel, both the signed-in
member and app must have access. After adding scopes, reinstall the Slack app
and reconnect OAuth. Use the manual ID fallback when necessary.

### A copied member value starts with `D`

That value is a direct-message conversation ID. Open the person's profile and
use **Copy member ID** to obtain the required `U...` value.

### A top-level message does not start a Flow

Check all of the following:

- the example Worker is running and connected to the current Dex Server;
- Socket Mode is enabled and the `xapp-...` token has `connections:write`;
- `message.channels` and `message.groups` are subscribed;
- the app was reinstalled after scope changes;
- the app is a member of the selected channel;
- the message is top-level, human-authored, and matches the configured channel,
  poster, and text filters.

### The Flow starts but an approval reply does nothing

Make sure the message is a reply in the original thread, the sender's `U...`
member ID is allowed, and the text matches the approval filter. Run only one
copy of the released example Worker. Connector v0.8.0 uses one shared Socket
Mode connection for both root and reply routes; separate competing connections
can consume each other's events.

### Reading the thread fails with a scope error

Add `channels:history` and `groups:history` under **User Token Scopes**, reinstall
the app, and reconnect in Dex Web so OAuth returns a new user token.

### `Processing complete.` is not posted

Confirm that the bot has `chat:write` and is a member of the channel. The example
wires only the happy-path branches. An unwired optional branch, including a
provider rejection or an uncertain write, fails the Flow and does not resend
the reply.

### Configuration changes do not affect the running Worker

Restart the example Worker. Local connection and Trigger configuration is
snapshotted during application startup. Refreshed OAuth credentials are loaded
before provider calls, but structural configuration requires a restart.

## Stop and clean up

Stop the Worker and dexcli with Control-C. Deleting local credentials in Dex Web
does not revoke the Slack grant. To revoke access, uninstall the test app from
the workspace or revoke it from Slack's app management page. Remove the local
test project only after retaining any Flow IDs and non-sensitive results needed
for the test report.
