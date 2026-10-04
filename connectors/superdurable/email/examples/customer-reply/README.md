# Email customer-reply example

This example answers one customer by email in a Flow started from Dex Web
**Start Flow**, using every email operation:

1. `RecordReplyRequest` validates the customer's address, the reply text, the
   subject for a new message, and the optional archive mailbox.
2. `FindCustomerMessages` calls `email.NewSearchMessagesStep` for INBOX
   messages whose From header contains the customer's address, ten per page,
   newest first.
3. `ChooseLatestCustomerMessage` keeps the newest summary sent by exactly the
   customer. IMAP `SEARCH FROM` matches a substring, so a lookalike such as
   `jane@acme.example.com.au` is skipped here. A page without an exact match
   leads to the next page; after the last page the Flow sends a new message.
4. `ReadCustomerMessage` calls `email.NewGetMessageStep`, which reads the
   message with `BODY.PEEK`, so reading never marks it seen.
5. `PrepareThreadedReply` quotes up to twenty lines of the message below the
   reply text, and `ReplyToCustomer` calls `email.NewReplyToMessageStep`. The
   reply carries `In-Reply-To`, `References`, and a `Re:` subject, and its
   Message-ID comes from the Step's idempotency key.
6. `MarkCustomerMessageAnswered` calls `email.NewSetFlagsStep` to set `\Seen`
   and `\Answered`.
7. With `archiveMailbox` set, `ArchiveCustomerMessage` calls
   `email.NewMoveMessageStep` with the message's Message-ID, so a repeated move
   finds the message already archived.
8. A customer without a message gets one new message from
   `SendNewCustomerMessage`, which calls `email.NewSendMessageStep`.

The `uncertain` branches of the reply and the new message are wired to
`RecordUncertainDelivery`: a submission whose answer was lost completes the
Flow with `needsReview: true`, the connector's `reviewDetail`, and the
Message-ID to look for, so a person checks the Sent mailbox or the recipient
instead of the Flow sending a possible duplicate. Every other optional branch
is unwired and fails the Flow, such as a refused login, a rejected recipient,
a missing message, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/superdurable/email` with the latest
stable dexcli release:

```bash
mkdir -p /tmp/email-reply/graphs /tmp/email-reply/release
dexcli visualize ./examples/customer-reply/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/email-reply/graphs/customer-reply
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

Dex needs release metadata built from this source, passed as an override;
without it the connection shows **Unsupported**. The connector has no Studio
bundle. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/superdurable/email/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/superdurable/email \
  --version v0.21.0 --tag connectors/superdurable/email/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/email-reply/release/connector-release.json \
  --digest-output /tmp/email-reply/release/connector-release.json.sha256
dexcli dev --open=false --flow-rendering-dir /tmp/email-reply/graphs \
  --connector-release-override email=/tmp/email-reply/release
```

Open Dex Web at `http://127.0.0.1:8802`, choose **Connections**, and select
`email / email-mailbox`, which all six email Steps of `EmailCustomerReply`
use. Follow the authorization guide on the page and the
[connection setup](../../README.md#connection-setup): enter the IMAP and SMTP
hosts, ports, and TLS modes from your provider's settings page, the login, and
an app password, then save; the status becomes **Ready** and **Local
override**. The example has no Step configuration. Dex Web or Superverse Studio
writes the connection to the project configuration.

Create the archive mailbox, such as `Archive`, in your mail client first; the
connector never creates mailboxes. In a second terminal, start the Worker from
`connectors/superdurable/email` with the `DEX_PROJECT_*` environment that names
that configuration, as
[project configuration loading](../../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
go run ./examples/customer-reply
```

The Worker reads the connection at startup and the credentials before every
call, so restart it after changing a host, port, or sender. The default Worker
address is `127.0.0.1:8834`. Override `DEX_WORKER_BIND_ADDRESS` or
`DEX_BLOB_CACHE_DIR` when needed. `EMAIL_TLS_ROOT_CA_FILE` names a PEM bundle
the Worker trusts instead of the system store, for a server with a private
certificate authority, such as a self-hosted server or a local test server.

In the Run workspace, choose **Start Flow**, select `EmailCustomerReply`,
choose the Worker at `127.0.0.1:8834`, enter a unique Flow ID, and submit:

```json
{
  "customerEmail": "jane@acme.example.com",
  "replyText": "Hi Jane,\n\nThe duplicate charge on order 88213 was refunded.",
  "newMessageSubject": "Your refund for order 88213",
  "archiveMailbox": "Archive"
}
```

Omit `archiveMailbox` to leave the answered message in INBOX. The Flow result
and the `email-customer-reply-outcome` Attribute hold the action (`replied`,
`sentNewMessage`, or `deliveryUncertain`), the answered message, the sent
Message-ID, the flags, the archive location, and the review state.

## Test

```bash
GOWORK=off go test -race ./examples/customer-reply/...
```

With the latest Dex development server running, the integration tests drive
the Flow on a real Worker against the in-process IMAP and SMTP servers in
`internal/mailtest`, which, like real servers, have no idempotency key:

- the customer's latest message gets one threaded reply and is marked and
  archived, while an older message, a lookalike sender, and another customer
  stay untouched;
- a customer with only a lookalike's message gets one new message;
- a match on a later search page is found;
- a reply and a new message whose server holds the answer to the final dot
  for nine seconds, past Dex's async local phase, are each submitted once,
  because both Steps are sync;
- a move held for nine seconds is dispatched again by async Dex, and the
  message is archived once;
- an answer lost after the final dot completes as `needsReview` and is never
  resubmitted;
- a Worker lost while the server holds the final dot is replaced, and the new
  attempt finds the submission checkpoint and submits nothing;
- a 451 refusal is retried and the reply is submitted once;
- a rejected recipient fails the Flow through the unwired `providerRejected`
  branch without the server's text;
- an invalid request fails the Flow before any connection.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
GOWORK=off go test -tags=integration ./examples/customer-reply/... -count=1 -v
```
