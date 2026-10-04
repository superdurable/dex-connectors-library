# Microsoft Outlook Mail Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack, through Dex Web and real-Dex tests, against a local Microsoft Graph stand-in; no Microsoft 365 tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Microsoft Outlook Mail Connector reads, organizes, and sends mail in a
Microsoft 365 (Exchange Online) mailbox through Microsoft Graph v1.0 at
`https://graph.microsoft.com/v1.0`. It exposes these operation-specific Dex
Step factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `outlookmail.NewSearchMessagesStep` | Query | async | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `outlookmail.NewGetMessageStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `outlookmail.NewSendMessageStep` | Mutation | sync | `sent` | `providerRejected`, `uncertain`, `defect` |
| `outlookmail.NewReplyToMessageStep` | Mutation | sync | `sent` | `notFound`, `providerRejected`, `uncertain`, `defect` |
| `outlookmail.NewMoveMessageStep` | Mutation | async | `moved` | `notFound`, `providerRejected`, `defect` |
| `outlookmail.NewSetMessageFlagsStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `defect` |

Operation names follow the [Email connector](../../superdurable/email)
(`searchMessages`, `getMessage`, `sendMessage`, `replyToMessage`,
`moveMessage`), so a Flow keeps its shape when it moves from IMAP to
Microsoft 365. Message references differ: Outlook uses one immutable Graph
message ID where IMAP needs a mailbox, UIDVALIDITY, and UID. The flag
operation is `setMessageFlags`, not the Email connector's `setFlags`, because
Outlook's state is a read flag, a follow-up flag, and categories rather than
IMAP's `\Seen`, `\Flagged`, and `\Answered`.

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Reads, moves, and
flag changes use a 30-second Execute timeout and six attempts within five
minutes. The two sends use sync durability, a 90-second Execute timeout with a
matching heartbeat timeout, and eight attempts within ten minutes; see
[Duplicate safety](#duplicate-safety).

The connector has one Studio picker, `mailFolderPicker`, and no Triggers; see
[Not in this release](#not-in-this-release).

## Connection setup

A connection is one mailbox and uses one of two methods. Dex Web
**Connections** shows the fields below; each description in `connector.yaml`
names where its value comes from.

| Field | Method | Secret | Default | Meaning |
| --- | --- | --- | --- | --- |
| `maxResponseBytes` | both | no | `4194304` | Largest Graph response one call reads |
| `client_id` | both | no | none, required | Application (client) ID of the Entra app registration |
| `client_secret` | both | yes | none, required | Client secret Value of the app registration |
| `access_token` | both | yes | output | Delegated: from consent; app-only: requested by the application |
| `refresh_token` | OAuth | yes | output | Microsoft's rotating refresh token |
| `tenant_id` | app-only | no | none, required | Tenant GUID or verified domain such as `contoso.onmicrosoft.com` |
| `mailbox` | app-only | no | none, required | Mailbox address, user principal name, or Entra object ID |

### Microsoft 365 account (OAuth), the default

A user signs in through Microsoft consent and every operation works on that
user's own mailbox (`/me`). **This method needs Dex CLI and Dex Web
`cli-v1.4.1` or later.** Microsoft does not echo `offline_access` in a token
response's `scope`, so `cli-v1.4.0` and earlier reject the consent with
`CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`; `cli-v1.4.1` (dex#584) accepts the
returned `refresh_token` as proof of `offline_access`.

1. Sign in to [Microsoft Entra](https://entra.microsoft.com) as a user who
   may register applications, open **Entra ID > App registrations > New
   registration**, and choose **Accounts in any organizational directory
   (Any Microsoft Entra ID tenant - Multitenant)**. Connector manifests
   declare static OAuth endpoints, so the connector signs in through
   `https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize`
   and `.../token`, which a single-tenant registration fails with error
   `AADSTS50194`. Personal Microsoft accounts (Outlook.com) are not supported.
2. Add the Redirect URI that Dex Web shows as a **Web** platform redirect.
3. Under **API permissions > Add a permission > Microsoft Graph > Delegated
   permissions**, add `Mail.ReadWrite` (search, read, drafts, move, flags,
   categories), `Mail.Send` (send the drafts), and `offline_access` (refresh
   tokens). Dex requests exactly these, in the short form Microsoft returns.
   Delegated `Mail.ReadWrite` and `Mail.Send` do not need administrator
   consent by default, but a tenant's user consent settings can require it;
   an administrator then chooses **Grant admin consent** on the same page.
4. Create a secret under **Certificates & secrets > Client secrets** and copy
   its **Value**, not its Secret ID, into `client_secret`; copy the
   Application (client) ID from **Overview**. Never paste a secret into a
   file in this repository; it is about 40 characters and Entra shows it once.
5. Choose **Authorize** and sign in with the mailbox's work or school account.

Dex Web exchanges the code with a form body that carries `client_id` and
`client_secret`; Microsoft documents that body for the code and refresh
grants, and the refresh driver sends it the same way. Refresh tokens rotate: the connector
stores the new one atomically on every refresh, and Microsoft can revoke one
after a password reset, an administrator's revocation, or long inactivity,
which selects `providerRejected` until the user authorizes again. The
refresh request repeats the consented scopes, and a refreshed token whose
`scope` lacks `Mail.ReadWrite` or `Mail.Send` requires reauthorization;
`offline_access` is never required there.

### App-only access to one mailbox

An Entra app authenticates as itself with the client credentials grant, for a
service or shared mailbox without a signed-in user. Every operation calls
`https://graph.microsoft.com/v1.0/users/<mailbox>`. The application requests
a token from `https://login.microsoftonline.com/<tenant_id>/oauth2/v2.0/token`
with the scope `https://graph.microsoft.com/.default`. The project connection
keeps the token with its expiry. A call requests a new token when none is
stored, as on the first call, or when the stored one expires within five
minutes. `tenant_id` must name one tenant: `common`, `organizations`,
and `consumers` are rejected before any request. A single-tenant app
registration works for this method.

Grant the app access to only that mailbox. Microsoft's least-privilege option
is [Exchange Online RBAC for Applications](https://learn.microsoft.com/en-us/exchange/permissions-exo/application-rbac),
which replaced application access policies. In Exchange Online PowerShell:

```powershell
New-ServicePrincipal -AppId <client_id> -ObjectId <Enterprise application object ID> -DisplayName "Outlook Mail for Dex"
New-ManagementScope -Name OutlookMailDex -RecipientRestrictionFilter "PrimarySmtpAddress -eq 'support@contoso.com'"
New-ManagementRoleAssignment -App <client_id> -Role "Application Mail.ReadWrite" -CustomResourceScope OutlookMailDex
New-ManagementRoleAssignment -App <client_id> -Role "Application Mail.Send" -CustomResourceScope OutlookMailDex
Test-ServicePrincipalAuthorization -Identity <client_id> -Resource support@contoso.com
```

`-ObjectId` is the object ID on **Entra ID > Enterprise applications > the
app**, not the one on App registrations. Exchange caches app permissions for
30 minutes to two hours, so a new assignment can take that long to apply;
`Test-ServicePrincipalAuthorization` bypasses the cache. Do not also grant the
Microsoft Graph application permissions
`Mail.ReadWrite` or `Mail.Send` in Entra: they reach every mailbox in the
tenant, and Exchange adds them to the scoped role assignments. Where RBAC for
Applications is unavailable, those two application permissions with **Grant
admin consent** also work, but they are tenant-wide. A request for another
mailbox, or one the role does not cover, selects `providerRejected` with
`AUTHORIZATION`.

## Project connection

Dex Web saves one connection per name in encrypted project storage. A
delegated connection's private credential holds `auth_method: microsoft-oauth`,
`client_id`, `client_secret`, and the `access_token` and `refresh_token`
from consent; an app-only credential holds `auth_method: app-only`,
`tenant_id`, `client_id`, `client_secret`, and the current `access_token`,
and the configuration holds `mailbox`.

Load the project configuration once at application startup and open the
connection by the name its operations use, as
[`examples/support-reply/main.go`](examples/support-reply/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := outlookmail.NewProjectConnection(project, supportreply.ConnectionName, connectionOptions()...)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

Credentials are reread before every operation, so a replaced secret takes
effect without a restart; the mailbox and the response limit are startup
configuration.

## Operations

Every request sends `Prefer: IdType="ImmutableId"`, so message IDs stay the
same when a message moves to another folder of the mailbox and when a draft
is sent and becomes its Sent Items copy. An ID changes only when the message
moves to an archive mailbox or is exported and imported. Folders are named by
ID or by Graph's well-known names, such as `inbox`, `archive`, `sentitems`,
and `deleteditems`. Every response is bounded by `$select` and by
`maxResponseBytes`.

### searchMessages

`searchMessages` lists one folder (blank uses `inbox`; subfolders are not
searched) with `$orderby=receivedDateTime desc` and a `$filter` built from:

- `from`: `from/emailAddress/address eq`, the exact sender address, so a
  lookalike such as `jane@acme.example.com.au` never matches;
- `subjectContains`: `contains(subject, ...)`;
- `receivedAfter` (inclusive) and `receivedBefore` (exclusive), to the second;
- `isUnread`: `isRead eq false`.

Graph requires a property in `$orderby` to come first in `$filter`, so every
filter starts with `receivedDateTime ge`. `limit` is 1 to 50
(`MaxSearchLimit`), 10 by default. `hasMore` and `nextPageCursor` continue the
search: the cursor is Graph's `@odata.nextLink`, accepted only when it is an
`https://graph.microsoft.com/v1.0/.../messages` link, and it carries the first
page's filters and size. Summaries carry the ID, conversation, Message-ID,
folder, subject, From, Sender, Reply-To, To and Cc (at most 50 each), received
and sent times, read, draft, follow-up flag, categories, importance,
attachments, and Graph's 255-character body preview.

### getMessage

`getMessage` reads one message with `Prefer: outlook.body-content-type="text"`,
so Graph converts an HTML body to text; if Graph returns HTML anyway, the
connector removes the markup itself (`textSource: html`). The text is cut at
64 KiB (`MaxTextBytes`) on a UTF-8 boundary with `isTextTruncated`. When the
message has attachments, a second request lists at most 50 with
`$select=id,name,contentType,size,isInline`, so no content is downloaded.
Reading never marks a message read.

### sendMessage

`sendMessage` sends one plain-text message to bare `to`, `cc`, and `bcc`
addresses (at least one `to`, at most 500 together, Exchange Online's limit)
with optional `replyTo`, a one-line subject of at most 255 characters, and a
body of at most 512 KiB. It creates a draft in Drafts with `POST /messages`,
carrying the marker below in the same request, and sends it with
`POST /messages/{id}/send`, which saves it to Sent Items. `SentMessage` names
the draft's immutable ID, which then reads the Sent Items copy.

### replyToMessage

`replyToMessage` creates Outlook's reply draft with `createReply`, or
`createReplyAll` with `isReplyAll`, passing the text as `comment`. Outlook
addresses the reply, adds `RE:` to the subject, threads it, and quotes the
original below the text. Graph v1.0 documents no request property for the
marker on `createReply`, so the connector adds it to the reply draft with
`PATCH`, which Graph allows on drafts, before sending. A missing original selects
`notFound` without drafting. The reply does not mark the original read; use
`setMessageFlags`.

### moveMessage

`moveMessage` reads the message's folder and resolves the destination's ID,
then moves only when they differ; a message already in the destination
selects `moved` with `wasAlreadyMoved` and writes nothing. A destination that
does not exist selects `providerRejected`; the connector never creates
folders.

### setMessageFlags

`setMessageFlags` sets `isRead`, the follow-up `flagStatus` (`notFlagged`,
`flagged`, or `complete`), and `categories` to absolute values; a nil field
leaves it alone, and an empty category list removes every category. It reads
the current values and sends one `PATCH` with only the differences;
categories compare as a set without case. When nothing differs it writes
nothing and selects `updated` with `wasAlreadyApplied`.

## Duplicate safety

Graph has no idempotency key for mail, and `sendMail`, `send`, and `reply`
answer `202 Accepted` with no body, so a lost answer leaves nothing to look
up. Dex re-dispatches an async Step whose local attempt passes about seven
seconds and retries a Step after a lost Worker, so each operation takes one
of these positions:

- **searchMessages** and **getMessage** only read.
- **moveMessage** and **setMessageFlags** keep async durability: both read
  before they write, the immutable ID survives the move, and the flags are
  absolute. Every unconfirmed outcome, including a 5xx or a dropped
  connection, is retried. The example's test holds a move for nine seconds,
  Dex dispatches the Step again, and the message is archived once.
- **sendMessage** and **replyToMessage** send through a draft and run with
  sync durability. The design combines a marker with a checkpoint:
  1. A draft carries the single-value extended property
     `IdempotencyMarkerPropertyID` (a named string property in this
     connector's own property set) whose value is `dex-` plus the Step's
     idempotency key, which every attempt of one Step execution shares.
  2. Before creating a draft, an attempt looks for one with its marker
     (`$filter=singleValueExtendedProperties/Any(...)` over the mailbox). A
     sent copy means an earlier attempt sent it; a draft is reused. So a lost
     answer to the draft creation never creates a second draft.
  3. After creating a draft, the attempt records a Dex heartbeat checkpoint
     with the draft ID, and before sending it records a second checkpoint,
     `sendDispatched`. A later attempt that finds `sendDispatched` never
     sends again: it reads the draft by its immutable ID, reports `sent` with
     `wasAlreadySent` once the Sent Items copy appears, and otherwise retries
     the read up to three times, three seconds apart, before selecting
     `uncertain` with the draft's ID and marker for a person to inspect.
  4. Only a provable refusal returns the checkpoint to the draft: a 429 or
     another answer Graph sends before doing anything is retried with the
     same draft, and a 4xx such as an invalid recipient selects
     `providerRejected` and leaves the unsent draft in Drafts.

Sync durability is what makes this safe; the marker alone is not, because two
concurrent attempts could both find no draft. The root integration test
overrides `sendMessage` to async durability against a fake Graph that holds
the send for nine seconds: Dex dispatches a second attempt, the local-phase
checkpoint is invisible to it, and it finds the marked draft and sends it
again, so the fake delivers twice. The same test with the default sync
durability sends once, as do the example's slow-send tests. Keep these Steps
sync.

Residual risks: Dex's `RecordHeartbeat` returns once the checkpoint enters
the Worker's stream, so a Worker lost in that instant could still allow one
repeated send of the same draft. A lost `createReply` answer leaves one
unmarked, unsent reply draft in Drafts, because that draft cannot be found
again; the retry drafts and sends once. Whether Exchange delivers twice when
the same draft is sent twice is undocumented and unverified.

The marker is chosen over Graph's other options: `sendMail` and `reply` return
no ID, a custom `x-` Internet header would reach recipients, and an
`internetMessageId` is assigned by Exchange, so a lost draft creation could
not be found by it.

## Errors

A Failure never contains Graph's or Microsoft's message text or a credential.
It names the status and Graph's `error.code`, such as `Microsoft Graph
rejected the request (HTTP 400 ErrorInvalidRecipients)`, and the Receipt
carries Graph's `request-id`.

| Answer | Reads | moveMessage and setMessageFlags | sendMessage and replyToMessage |
| --- | --- | --- | --- |
| 429 or 509 | Retry after `Retry-After` | Retry | Retry; a refused send keeps the same draft |
| 408, 409, 423 | Retry | Retry | Retry |
| 500, 502, 503, 504, dropped connection after dispatch | Retry after `Retry-After` | Retry | Retry before the send; after it, confirm from the draft, then `uncertain` |
| Connection refused | Retry | Retry | Retry |
| 401 | `providerRejected`, `AUTHENTICATION`, after one refresh and resend only when the stored token has expired | same | same |
| 403 | `providerRejected`, `AUTHORIZATION` (`QUOTA_EXHAUSTED` for a quota code) | same | same; nothing sent |
| 404 `ErrorItemNotFound`, 400 `ErrorInvalidIdMalformed` | `notFound` | `notFound` | `notFound` for the reply original |
| other 404, such as an unknown mailbox | `providerRejected`, `NOT_FOUND` | same | same |
| 507 | `providerRejected`, `QUOTA_EXHAUSTED` | same | same |
| other 4xx, 501, 3xx | `providerRejected` | same | same; nothing sent |
| Oversized, malformed, or token-reflecting 2xx | `invalidResponse` | Retry | Retry before the send; a reply draft answer is `providerRejected` |
| Revoked grant (`invalid_grant`, `invalid_client`, `interaction_required`, ...) | `providerRejected`, `AUTHENTICATION` | same | same |
| Token endpoint outage | Retry | Retry | Retry |
| Invalid input or connection | `defect`, no request | same | same |

Microsoft documents the error envelope, the status codes, and 429 with
`Retry-After`; it does not document the Outlook `error.code` values above,
which follow Exchange's observed codes.

Graph throttles each app and mailbox at 10,000 requests in ten minutes and
four concurrent requests. A first send attempt makes three Graph requests and
a reply four, so run at most a few send Steps at once per mailbox.

## Studio picker

`mailFolderPicker` lists the mailbox's top-level folders with Graph's
`mailFolders` and a chosen folder's subfolders with `childFolders`, both read
with the connection's `access_token` through Dex Web's broker. A delegated
connection lists `/me`; an app-only connection lists `/users/{mailbox}` from
its saved `mailbox` field and needs the access token stored in the project
connection. The unit stores `folderId` and `folderName` and always offers a
folder ID or well-known name as manual entry, which is also the fallback when
the list cannot load. Studio commands are `GET` requests to the fixed
`graph.microsoft.com` host with path parameters only, so the host cannot be
templated; Graph satisfies that.

## Not in this release

### Triggers

A `messageReceived` Trigger is deferred. Graph change notifications for
`/me/mailFolders('inbox')/messages` need a public HTTPS endpoint that answers
Graph's `validationToken` handshake in plain text within ten seconds when the
subscription is created, acknowledges each notification within three
seconds, and verifies its `clientState`. A subscription for Outlook messages
lasts under seven days (10,080 minutes, or 1,440 with resource data), so the
source must renew it, and it must handle the `reauthorizationRequired`,
`subscriptionRemoved`, and `missed` lifecycle notifications, the last by
resynchronizing. `sdkgo/webhooktrigger` has no handshake hook yet. Polling
with delta query instead needs a durable per-binding, per-folder
`@odata.deltaLink` cursor that survives a restart and moves only after
`sdkgo.PrepareTriggerDelivery` recorded the event; `sdkgo` has no such store.
Until then, run `searchMessages` with `isUnread` and `receivedAfter` from a
Flow on a Timer and mark handled messages with `setMessageFlags`.

### Other limits

HTML or attachments in sent mail, sending as another mailbox (`Send As` or
shared mailboxes for a delegated connection), searching subfolders or every
folder, `$search` (KQL), personal Microsoft accounts, certificate credentials
for app-only, national clouds, and the archive mailbox are not supported.

## Example

[`examples/support-reply`](examples/support-reply) is a runnable Dex Web
**Start Flow** example that uses all six operations: it finds the customer's
latest Inbox message, reads it, sends one threaded reply, marks it read, and
moves it to the folder picked in Dex Web, or sends a new message to a
customer without one.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the module owns its real
Worker, retry, persistence, duplicate-dispatch, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest, generated code, and bundle:

```bash
go run ./cmd/connectorctl validate connectors/microsoft/outlook-mail/connector.yaml
go run ./cmd/connectorctl generate --check connectors/microsoft/outlook-mail/connector.yaml
(cd connectors/microsoft/outlook-mail/ui && npm ci && npm test && npm run build)
python3 script/studio_bundle_theme_check.py . --selected connectors/microsoft/outlook-mail
```

The tests run `internal/graphtest`, a stateful Graph and identity platform
stand-in that keeps immutable IDs, stores and filters extended properties,
requires the immutable-ID preference on every request and `Content-Length: 0`
on a send, rotates refresh tokens, issues app-only tokens for one tenant,
limits app tokens to one mailbox, and can hold, refuse, or drop any request
before or after applying it. They cover filter order and OData quoting,
cursor validation, text and HTML bodies, truncation, attachment listing, the
draft marker, every send checkpoint transition, lost create, send, and
`createReply` answers, a Sent Items copy that appears late or never, a lost
Worker, a 401 refresh with rotation, a revoked grant, both token grants and
tenant validation, scope checks, redirects, token reflection, oversized
answers, and error mapping without server text.

No Microsoft 365 tenant was used. The following live behavior is unverified:
the multitenant consent with the three scopes and the exact `scope` string
Microsoft returns; refresh with the repeated `scope` parameter;
`contains(subject, ...)` together with `$orderby` on large mailboxes (Graph
documents neither `contains` on `subject` nor its cost); filtering messages by
the extended property and that the property and the immutable ID survive the
send to Sent Items (documented, not exercised); how fast Exchange moves a
sent draft to Sent Items; Exchange's answer to sending one draft twice;
`createReply` and `createReplyAll` addressing, which return 201 in Graph's
text and 200 in its example; the `Prefer` header carrying two preferences;
the Outlook `error.code` values in [Errors](#errors); RBAC for Applications
with these cmdlets; the folder picker against a real mailbox; and the Entra
admin center paths in the setup guidance.
