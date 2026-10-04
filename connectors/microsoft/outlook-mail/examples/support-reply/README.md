# Outlook Mail support-reply example

This example answers one customer from a Microsoft 365 mailbox in a Flow
started from Dex Web **Start Flow**, using every Outlook Mail operation:

1. `RecordReplyRequest` validates the customer's address, the reply text, and
   the subject for a new message.
2. `FindCustomerMessages` calls `outlookmail.NewSearchMessagesStep` for Inbox
   messages whose From address is exactly the customer's, ten per page,
   newest first.
3. `ChooseLatestCustomerMessage` keeps the newest summary from the customer.
   A page without one leads to the next page through its cursor; after the
   last page the Flow sends a new message.
4. `ReadCustomerMessage` calls `outlookmail.NewGetMessageStep`, which reads
   the message as plain text without marking it read and lists its
   attachments.
5. `PrepareReply` records the attachment count, and `ReplyToCustomer` calls
   `outlookmail.NewReplyToMessageStep`. Outlook's reply draft threads the
   reply and quotes the original; the draft carries the Step's marker and is
   sent at most once.
6. `MarkCustomerMessageRead` calls `outlookmail.NewSetMessageFlagsStep` to
   mark the message read.
7. `ArchiveCustomerMessage` calls `outlookmail.NewMoveMessageStep` to move
   the message to the folder picked in Dex Web, or to the well-known Archive
   folder until one is picked. The message ID is immutable, so a repeated
   move finds the message already there.
8. A customer without an Inbox message gets one new message from
   `SendNewCustomerMessage`, which calls `outlookmail.NewSendMessageStep`.

The `uncertain` branches of the reply and the new message are wired to
`RecordUncertainDelivery`: a send whose answer was lost and whose Sent Items
copy never appeared completes the Flow with `needsReview: true`, the
connector's `reviewDetail`, and the draft's ID and marker, so a person checks
the mailbox instead of the Flow sending a possible duplicate. Every other
optional branch is unwired and fails the Flow, such as a revoked
authorization, a rejected recipient, a missing message, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/microsoft/outlook-mail` with the latest
stable dexcli release:

```bash
mkdir -p /tmp/outlook-mail-reply/graphs /tmp/outlook-mail-reply/release
dexcli visualize ./examples/support-reply/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/outlook-mail-reply/graphs/support-reply
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

Dex needs release metadata and the Studio bundle built from this source,
passed as an override; without it the connection shows **Unsupported**. A
delegated Microsoft OAuth connection also needs Dex CLI and Dex Web
`cli-v1.4.1` or later; see the
[connector README](../../README.md#microsoft-365-account-oauth-the-default).
From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
npm ci --prefix sdk/react && npm run build --prefix sdk/react
npm ci --prefix connectors/microsoft/outlook-mail/ui && npm run build --prefix connectors/microsoft/outlook-mail/ui
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/microsoft/outlook-mail/connector.yaml \
  --ui-root connectors/microsoft/outlook-mail/ui/dist \
  --output /tmp/outlook-mail-reply/release/connector-ui.tgz \
  --digest-output /tmp/outlook-mail-reply/release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/microsoft/outlook-mail/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/microsoft/outlook-mail \
  --version v0.21.0 --tag connectors/microsoft/outlook-mail/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/outlook-mail-reply/release/connector-ui.tgz \
  --ui-digest /tmp/outlook-mail-reply/release/connector-ui.tgz.sha256 \
  --output /tmp/outlook-mail-reply/release/connector-release.json \
  --digest-output /tmp/outlook-mail-reply/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/outlook-mail-reply/graphs \
  --connector-release-override outlook-mail=/tmp/outlook-mail-reply/release
```

Open Dex Web at `http://127.0.0.1:8802`, choose **Connections**, and select
`outlook-mail / outlook-support-mailbox`, which all six Outlook Mail Steps of
`OutlookSupportReply` use. Follow the authorization guide on the page and the
[connection setup](../../README.md#connection-setup):

- **Microsoft 365 account (OAuth)**: enter the multitenant app's client ID and
  secret, register the Redirect URI the page shows, and choose
  **Authorize**; the Flow then answers from that user's mailbox.
- **App-only access to one mailbox**: enter the tenant ID, client ID, client
  secret, and mailbox, leave `access_token` blank, and save; grant the app the
  mailbox with RBAC for Applications first.

Dex Web keeps the secrets in encrypted project storage; they never enter a
Flow.

## Step configuration

| Step | Unit | Saved value | Blank |
| --- | --- | --- | --- |
| `ArchiveCustomerMessage` | `mailFolderPicker` as **Archive folder** | `{"folderId": "<Graph folder ID>", "folderName": "Inbox / Answered"}` | moves answered messages to the well-known Archive folder |

On the same connection page, the **Archive folder** unit lists the mailbox's
folders with **Load folders**; choose **Show subfolders** to go deeper, pick
the folder that receives answered messages, and save, or enter a folder ID or
a well-known name such as `archive`. The folder must already exist. An
app-only connection lists folders only after the Worker has stored a token,
so run the Flow once first or type the folder.

In a second terminal, start the Worker from `connectors/microsoft/outlook-mail`.
It reads the `DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/support-reply
```

The Worker reads the connection and the picked folder at startup and the
credentials before every call, so restart it after saving the folder or
changing the mailbox. The default Worker address is `127.0.0.1:8837`.
Override `DEX_WORKER_BIND_ADDRESS` or `DEX_BLOB_CACHE_DIR` when needed. For
local verification against a Graph-compatible stand-in only,
`OUTLOOK_MAIL_LOCAL_PROVIDER_URL` sends every request for
`graph.microsoft.com` and `login.microsoftonline.com` to one loopback URL
without a path; production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `OutlookSupportReply`,
choose the Worker at `127.0.0.1:8837`, enter a unique Flow ID, and submit:

```json
{
  "customerEmail": "jane@acme.example.com",
  "replyText": "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.",
  "newMessageSubject": "Your refund for order 88213"
}
```

The Flow result and the `outlook-support-reply-outcome` Attribute hold the
action (`replied`, `sentNewMessage`, or `deliveryUncertain`), the answered
message, the attachment count, the sent message's ID, subject, recipients,
and marker, the message state, the archive folder, and the review state. The
run sends real mail from the mailbox; use a test customer address.

## Test

```bash
GOWORK=off go test -race ./examples/support-reply/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against `internal/graphtest`, which, like Graph, has
no send idempotency key:

- the customer's latest message gets one threaded reply, is marked read, and
  is archived under the same immutable ID, while an older message, a
  lookalike sender, and another customer stay untouched;
- a folder picked in Dex Web receives the answered message;
- a customer with only a lookalike's message gets one new message;
- a reply and a new message whose send Graph holds for nine seconds, past
  Dex's async local phase, are each sent once, because both Steps are sync;
- a move held for nine seconds is dispatched again by async Dex, and the
  message is archived once;
- a send whose answer is lost after Graph applied it is confirmed from the
  Sent Items copy and never sent again;
- a send whose answer is lost and whose copy never appears completes as
  `needsReview` without a second send;
- a Worker lost while Graph holds the send is replaced, and the new attempt
  finds the dispatch checkpoint and confirms the send instead of sending;
- an expired access token is refreshed during the Flow and the rotated
  refresh token is stored;
- a rejected recipient fails the Flow through the unwired `providerRejected`
  branch without Graph's text;
- an invalid request fails the Flow before any Graph request.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
GOWORK=off go test -tags=integration ./examples/support-reply/... -count=1 -v
```
