# Email (IMAP and SMTP) Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against in-process IMAP and SMTP servers; no real mail provider was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Email Connector reads and organizes messages in any IMAP mailbox and sends
plain-text messages and threaded replies over SMTP, for mail providers other
than Gmail and Microsoft 365. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `email.NewSearchMessagesStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `email.NewGetMessageStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `email.NewSendMessageStep` | Mutation | sync | `sent` | `providerRejected`, `uncertain`, `defect` |
| `email.NewReplyToMessageStep` | Mutation | sync | `sent` | `notFound`, `providerRejected`, `invalidResponse`, `uncertain`, `defect` |
| `email.NewMoveMessageStep` | Mutation | async | `moved` | `notFound`, `providerRejected`, `defect` |
| `email.NewSetFlagsStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. IMAP operations use
a 30-second Execute timeout and five attempts within two minutes. The two SMTP
operations use sync durability, a 150-second Execute timeout with a matching
heartbeat timeout, and five attempts within ten minutes; see
[Duplicate safety](#duplicate-safety).

Use the [Gmail connector](../../google/gmail) for Gmail. Microsoft 365
(Exchange Online) turned off password sign-in for IMAP and requires OAuth 2.0,
which this release does not offer: XOAUTH2 and OAUTHBEARER are out of scope.
This release has no Triggers and no Studio pickers; see
[Not in this release](#not-in-this-release).

## Connection setup

A connection is one mailbox. Dex Web **Connections** shows these fields, and
each description in `connector.yaml` names where to find the value.

| Field | Secret | Default | Meaning |
| --- | --- | --- | --- |
| `imapHost` | no | none, required | IMAP host name or IP address, without a scheme or port |
| `imapPort` | no | `993` | IMAP port |
| `imapSecurity` | no | `implicitTLS` | `implicitTLS` or `startTLS` |
| `smtpHost` | no | none, required | SMTP submission host |
| `smtpPort` | no | `465` | SMTP port |
| `smtpSecurity` | no | `implicitTLS` | `implicitTLS` or `startTLS` |
| `fromAddress` | no | `username` | Sender address for From and the SMTP envelope |
| `fromName` | no | none | Display name beside the sender |
| `username` | no | none, required | IMAP login, and the SMTP login unless `smtp_username` is set |
| `password` | yes | none, required | App password, and the SMTP password unless `smtp_password` is set |
| `smtp_username` | no | `username` | SMTP login for a separate relay or a provider with a different SMTP login |
| `smtp_password` | yes | `password` | SMTP password for a separate relay |

TLS is required. `implicitTLS` starts TLS as soon as the connection opens,
which provider pages call SSL/TLS; `startTLS` connects and upgrades with the
STARTTLS command before any credential is sent. The connector never logs in or
authenticates over plaintext: a server that does not offer STARTTLS selects
`providerRejected` before the login, and a connection is never downgraded. It
verifies the server certificate and host name against the Worker's system
trust store with TLS 1.2 or later. `email.WithTLSRootCAs` replaces the trust
store for a server with a private certificate authority, such as a self-hosted
server or a local mail bridge; the example reads a PEM bundle from
`EMAIL_TLS_ROOT_CA_FILE` for this.

IMAP logs in with `LOGIN`, or with `AUTHENTICATE PLAIN` when the server
advertises `LOGINDISABLED`. SMTP authenticates with `AUTH PLAIN`, or with
`AUTH LOGIN` when the server offers only that.

These provider settings were read from the providers' help pages on
2026-10-01:

| Provider | IMAP | SMTP | Login and password |
| --- | --- | --- | --- |
| Fastmail ([settings](https://www.fastmail.help/hc/en-us/articles/1500000278342)) | `imap.fastmail.com` 993, SSL/TLS | `smtp.fastmail.com` 465 SSL/TLS, or 587 STARTTLS | Complete address; an app password from **Settings > Privacy & Security > Connected apps & API tokens > Manage app passwords and access > New app password** with **Mail, Contacts & Calendars** access. Basic plans have no IMAP or SMTP. |
| iCloud Mail ([settings](https://support.apple.com/en-us/102525)) | `imap.mail.me.com` 993, SSL | `smtp.mail.me.com` 587, STARTTLS | IMAP login can be the name before the @; SMTP needs the complete address, so set `smtp_username`. App-specific password from **account.apple.com > Sign-In and Security > App-Specific Passwords**, which needs two-factor authentication. |
| Yahoo Mail ([settings](https://help.yahoo.com/kb/SLN4075.html)) | `imap.mail.yahoo.com` 993, SSL | `smtp.mail.yahoo.com` 465 or 587 | Complete address; an app password from **login.yahoo.com/account/security > External connections > Create app password**. |

Revoke access by deleting the app password at the provider; every operation
then selects `providerRejected` until a new one is saved. Credentials are
reread before every operation, so a replaced password takes effect without a
restart. Hosts, ports, TLS modes, and the sender are startup configuration.

## Project configuration

Dex Web or Superverse Studio writes the connection to the project
configuration. Load it with `projectconfig.LoadFromEnvironment` and open the
connection by the name the application uses with `email.NewProjectConnection`,
as [`examples/customer-reply/main.go`](examples/customer-reply/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
options, err := connectionOptions()
if err != nil {
	return err
}
connection, err := email.NewProjectConnection(project, customerreply.ConnectionName, options...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `username`, `password`, and the optional
`smtp_username` and `smtp_password`.

## Libraries

- IMAP uses `github.com/emersion/go-imap/v2` v2.0.0-beta.8 (December 2025), the
  maintained line of the most widely used pure-Go IMAP library; v1 is in
  maintenance only. It provides the client, `UID MOVE` with a `UIDPLUS`
  fallback, `COPYUID`, ESEARCH, literal and UTF-8 search encoding, and the
  in-memory server the tests use. Its tag is a prerelease, so a later tag can
  change its API; the module pins this exact version.
- SMTP uses `github.com/emersion/go-smtp` v0.25.0 (August 2026) instead of the
  frozen `net/smtp`, because it returns the reply code and enhanced status code
  as `*smtp.SMTPError`, separates the final dot's answer from the writes before
  it, and includes the server the tests use.
- `github.com/emersion/go-message` v0.18.2 encodes headers and quoted-printable
  bodies and decodes received charsets and transfer encodings, and
  `github.com/emersion/go-sasl` provides PLAIN and LOGIN. go-sasl has no
  release tags; the module pins the commit that go-imap and go-smtp require.
- `golang.org/x/net/html`, already in the dependency graph through gRPC,
  tokenizes HTML bodies.

The four emersion modules are pure Go and declare `go 1.18` or earlier; the
module builds and vets with `GOTOOLCHAIN=go1.24.0`.

## Operations

Every message is identified by a `MessageReference`: the mailbox, its
UIDVALIDITY, and the UID. A UID names the same message only while the mailbox
keeps its UIDVALIDITY, so every operation that takes a reference checks it and
selects `notFound` when it changed. Messages are returned with their
Message-ID without angle brackets.

### searchMessages

`searchMessages` examines one mailbox read-only (blank uses `INBOX`) and runs
`UID SEARCH` with these typed filters, all of which must match:

- `from`, `to`, and `subjectContains`: IMAP `FROM`, `TO`, and `SUBJECT`, a
  case-insensitive substring of the header. Servers with full-text indexes,
  such as Dovecot with FTS, may match whole words. `from: jane@acme.example.com`
  also matches `jane@acme.example.com.au`, so confirm the exact sender in the
  Flow, as the example does. Non-ASCII text is sent with `CHARSET UTF-8`.
- `sinceDate` and `beforeDate`: `YYYY-MM-DD`, IMAP `SINCE` and `BEFORE`, which
  compare the server's arrival date in the server's time zone and ignore the
  time of day.
- `isUnseen`: only messages without `\Seen`.

Results are newest first by UID, which is arrival order in the mailbox, not
the Date header. `limit` is 1 to 50 (`MaxSearchLimit`), 10 by default.
`totalMatched` counts every match, and `hasMore` and `nextOlderThanUid`
continue the search: pass `olderThanUid` with `uidValidity`, and a changed
UIDVALIDITY selects `providerRejected` with `CONFLICT`. Summaries carry the
reference, Message-ID, In-Reply-To, From, Reply-To, To, Cc (at most 50 each),
subject (cut at 998 bytes), Date, arrival time, `\Seen`, `\Flagged`,
`\Answered`, size, and whether there are attachments. They carry no body:
read one with `getMessage`.

### getMessage

`getMessage` reads one message with `BODY.PEEK`, so reading never sets
`\Seen`. It returns the summary fields, the last 20 References identifiers,
and the first `text/plain` part decoded to UTF-8 from its transfer encoding
and charset. A message without a plain part returns the text of its first
`text/html` part with tags, scripts, and styles removed (`textSource: html`).
At most 1 MiB of the encoded part is read, and the text is cut at 64 KiB
(`MaxTextBytes`) on a UTF-8 boundary with `isTextTruncated`. Attachments are
listed without content: body section, file name, media type, and size in
their transfer encoding (base64 is about four thirds of the file). A part
with an attachment disposition, a file name, or an encapsulated message
counts as an attachment.

### sendMessage

`sendMessage` submits one plain-text UTF-8 message to `to`, `cc`, and `bcc`
(bare ASCII addresses, at least one `to`, at most 50 in all) with an optional
`replyTo`. Bcc recipients receive the message but appear in no header. The
subject is one line; non-ASCII headers are encoded, and the body is sent as
quoted-printable, at most 512 KiB. The From header is `fromName` and
`fromAddress`, or `username` when `fromAddress` is blank and `username` is a
complete address.

The Message-ID is `dex-<idempotency key>@<sender domain>`. The key comes from
the Dex Call ID, so every attempt of one Step execution writes the same
Message-ID, and an `uncertain` Result names the message to look for. The
message is not appended to a Sent mailbox; some providers save SMTP
submissions there and others do not.

### replyToMessage

`replyToMessage` reads the source message's envelope and References header
over IMAP without setting `\Seen`, then submits the reply like `sendMessage`:

- it goes to the source's Reply-To, which IMAP servers report as From when the
  message has none; `isReplyAll` also addresses the original To and Cc, and
  `cc` adds more. The connection's own sender is never addressed;
- `In-Reply-To` is the source Message-ID, and `References` is the source's
  References plus its Message-ID, the last 20;
- the subject gets `Re: ` unless it already starts with `Re:` in any case;
- the text is sent as given; quote the original in it, as the example does.

A missing source selects `notFound`, and a source without a usable reply
address selects `invalidResponse`; neither sends anything. The reply does not
set `\Answered`; use `setFlags`.

### moveMessage

`moveMessage` moves one message to an existing mailbox with `UID MOVE`. On a
server with `UIDPLUS` but without `MOVE`, it uses `UID COPY`, `STORE \Deleted`,
and `UID EXPUNGE` of that UID only. A server with neither is refused with
`providerRejected`, because a plain `EXPUNGE` would remove other deleted
messages. Set `messageId` from the search result: it must match the message
at the UID, and an attempt that finds the UID gone looks for that Message-ID
in the destination and selects `moved` with `wasAlreadyMoved`. The result
names the destination UID when the server returns `COPYUID`. A destination
that does not exist selects `providerRejected` with `NOT_FOUND`; the connector
never creates mailboxes.

### setFlags

`setFlags` sets or clears `\Seen`, `\Flagged`, and `\Answered` to absolute
values; a nil field leaves that flag alone. It reads the flags, stores only
the differences, and reads them back. When nothing differs it writes nothing
and selects `updated` with `wasAlreadyApplied`. A server that accepts the
change but does not keep it selects `providerRejected`.

## Duplicate safety

SMTP has no idempotency key, so a repeated submission is a second email to the
customer. Dex re-dispatches an async Step whose local attempt passes about
seven seconds, and it retries a Step after a lost Worker, so the operations
take these positions:

- **searchMessages** and **getMessage** only read.
- **setFlags** and **moveMessage** are safe to repeat and keep async
  durability. Flags are absolute values from a fresh read, and a message can
  be moved only once; a repeat that finds it gone finds it by Message-ID in the
  destination. Every unconfirmed outcome, including a lost connection, is
  retried. The example's test holds a `MOVE` for nine seconds, Dex dispatches
  it again, and the message is archived once.
- **sendMessage** and **replyToMessage** run with sync durability, so Dex never
  sends a second attempt while the first is in flight, even when the server
  takes seconds to answer the final dot. Before connecting to the SMTP server,
  the operation records a Dex heartbeat checkpoint naming the Step's Call ID.
  A later attempt of the same Step execution that finds the checkpoint, after a
  lost Worker or an Execute timeout, selects `uncertain` without connecting.
  `replyToMessage` checks for the checkpoint before it reads the source.
- An SMTP server accepts a message only when it answers the final dot, so
  every failure before that point is provably not sent: a 4xx reply or a
  connection that failed before the final dot clears the checkpoint and is
  retried, and a 5xx clears it and selects `providerRejected`. A 4xx or 5xx
  answer to the final dot is also a refusal. Only an answer lost after the
  final dot, such as a dropped connection or a timeout, selects `uncertain`,
  with the Message-ID to look for; the connector never resubmits it.

Keep these two Steps sync. The root integration test overrides
`sendMessage` to async durability against a server that holds the final dot
for nine seconds and observes two submissions, because Dex's local-phase
heartbeats are not visible to the fallback attempt. Dex's `RecordHeartbeat`
returns once the checkpoint enters the Worker's stream, not after the server
stores it, so a Worker lost in that instant could still allow one resubmission;
it would carry the same Message-ID. A crash after the checkpoint and before
the final dot makes an attempt report `uncertain` for a message the server
never accepted; that direction is safe.

## Errors

A Failure never contains the server's human-readable text or a credential.
IMAP failures name the command, the status, and the response code, such as
`the IMAP server answered LOGIN with NO [AUTHENTICATIONFAILED]`; SMTP failures
name the command and the reply and enhanced status codes, such as
`the SMTP server answered RCPT TO for recipient 2 of 2 with 550 5.1.1`.

| Outcome | Reads | moveMessage and setFlags | sendMessage and replyToMessage |
| --- | --- | --- | --- |
| Host does not resolve | `providerRejected` | `providerRejected` | `providerRejected` |
| Connection refused, lost, or timed out | Retry | Retry | Retry before the final dot; `uncertain` after it |
| Certificate fails verification, no TLS on the port, or no STARTTLS offered | `providerRejected` | `providerRejected` | `providerRejected` |
| Login refused (`AUTHENTICATIONFAILED`, SMTP 535) | `providerRejected`, `AUTHENTICATION` | same | same |
| Mailbox missing (`NONEXISTENT`, `TRYCREATE`) | `providerRejected` for search, `notFound` for a reference | `notFound`; a missing move destination is `providerRejected` | `notFound` for the reply source |
| UID missing or UIDVALIDITY changed | `notFound` | `notFound` | `notFound` for the reply source |
| IMAP `UNAVAILABLE`, `INUSE`, `SERVERBUG`, `LIMIT`, or `BYE` | Retry | Retry | Retry |
| Other IMAP `NO` or `BAD` | `providerRejected` | `providerRejected` | `providerRejected` |
| SMTP 4xx | n/a | n/a | Retry, checkpoint cleared |
| SMTP 5xx, such as a rejected recipient or sender | n/a | n/a | `providerRejected`, nothing sent |
| Unparseable server response | `invalidResponse` | Retry | `invalidResponse` for the reply source; `providerRejected` for an unusable SMTP answer before the final dot |
| Invalid input or connection credentials | `defect`, no connection | `defect` | `defect` |

## Not in this release

### messageReceived Trigger

A poll Trigger for new mail needs a durable per-binding cursor of the
mailbox's UIDVALIDITY and last delivered UID that survives a restart and moves
only after `sdkgo.PrepareTriggerDelivery` recorded the event. The SDK has no
such store: `TriggerSource` and `projectconfig`'s durable Trigger inbox
persist only events awaiting delivery and remove them once consumed. Gmail's
poll Trigger keeps its delivered set in memory and rescans the newest page
after a restart, relying on Flow-start request IDs to drop duplicates; for IMAP
that would redeliver up to a page of old mail after every restart and silently
skip mail that arrived while more than a page behind. The Trigger is deferred
until the SDK offers a durable source cursor in `projectconfig`. Until then,
run `searchMessages` with `isUnseen` and `sinceDate` from a Flow on a Timer and
mark handled messages with `setFlags`.

### Studio pickers

A mailbox picker would need `LIST` over IMAP, but Studio setup commands
support only HTTPS `GET` requests to a fixed host, so mailbox names are
operation input.

### Other limits

OAuth 2.0 (XOAUTH2), HTML or multipart messages and attachments in sent mail,
saving a copy to a Sent mailbox, IMAP `IDLE`, and non-ASCII (SMTPUTF8)
addresses are not supported.

## Example

[`examples/customer-reply`](examples/customer-reply) is a runnable Dex Web
**Start Flow** example that uses all six operations: it finds the customer's
latest message, reads it, sends one threaded reply that quotes it, marks it
seen and answered, and archives it, or sends a new message to a customer
without one.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the module owns its real
Worker, retry, persistence, duplicate-submission, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/superdurable/email/connector.yaml
go run ./cmd/connectorctl generate --check connectors/superdurable/email/connector.yaml
```

The tests run go-imap's in-memory IMAP server and go-smtp's server in
process, over TLS with a throwaway certificate authority (`internal/mailtest`).
They cover implicit TLS and STARTTLS, refusal of plaintext servers and
untrusted certificates, LOGIN and AUTH failures without server text,
search filters, paging, UTF-8 search, UIDVALIDITY checks, decoded
quoted-printable, base64, ISO-8859-1, and HTML bodies with bounds, attachment
listing, header encoding, Bcc privacy, dot-stuffing, the submission checkpoint
and its clearing, temporary and permanent SMTP refusals, a lost answer to the
final dot, `MOVE` and its `UIDPLUS` fallback, a lost `MOVE` reply, and flag
changes.

No live mail provider was used. The following live behavior is unverified:
the Fastmail, iCloud Mail, and Yahoo Mail logins, app passwords, and certificate
chains; whether each provider's `SEARCH FROM` matches substrings or words;
whether iCloud accepts the complete address as the IMAP login; whether
providers answer the final dot within the 90-second limit; which providers
save SMTP submissions to a Sent mailbox; `COPYUID` and `MOVE` support on each
provider; and the provider console paths in the setup guidance.
