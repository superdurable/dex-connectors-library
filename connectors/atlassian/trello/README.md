# Trello Connector

> **Verification status: partial live.** Only a dummy-credential request reached Trello, which rejected it; everything else ran on a real Dex stack against a local stand-in. No live Trello account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module lists, reads, creates, and updates Trello cards and comments on them
through the Trello REST API, `https://api.trello.com/1`:

| Operation | Kind | Trello endpoint | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `listCards` | Query | `GET /boards/{id}/cards` or `GET /lists/{id}/cards` | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `getCard` | Query | `GET /cards/{id}` | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `createCard` | Mutation | `POST /cards` | sync | `created` | `providerRejected`, `uncertain`, `defect` |
| `updateCard` | Mutation | `PUT /cards/{id}`, then `GET /cards/{id}` | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `addComment` | Mutation | `POST /cards/{id}/actions/comments` | sync | `added` | `notFound`, `providerRejected`, `uncertain`, `defect` |

Trello has no status field: a card's list is its workflow state, so moving a
card to another list is its status change, and `updateCard` treats a move as a
first-class change. Archiving is the separate `closed` flag.

IDs are Trello's 24-character hexadecimal IDs, such as
`5b6893f01cb3228998cf629e`. The 8-character short link in a card's web address
is not accepted, because Trello's API reference types every `{id}` as the
24-character ID. Results, receipts, failures, and logs never contain the key,
the token, or Trello's error text: Trello answers most errors with plain-text
bodies such as `invalid token` that can repeat request content, so a rejection
names only the HTTP status, such as `Trello rejected the card with HTTP 400`.
Members carry an ID, full name, and username, never an email address.

No Trigger and no Studio picker ships in this release; see
[Not in this release](#not-in-this-release).

## Authentication

A connection holds two secrets: the API key of a Trello app and a user token
that authorizes the app to act as one Trello member. Every request sends both in
Trello's documented header, so neither appears in a URL:

```text
Authorization: OAuth oauth_consumer_key="{apiKey}", oauth_token="{apiToken}"
```

1. Sign in to Trello as the member whose access the Flows should use. Cards and
   comments are created as that member, on every board the member can open; a
   dedicated member narrows what a leaked token can reach.
2. Open the app admin portal at <https://trello.com/apps/admin>, choose
   **New**, accept the Joint Development Agreement if Trello asks, name the
   app, and choose **Create**. An app that only calls the REST API does not
   need Power-Up capabilities.
3. Open the app, select the **Trello Auth** tab, choose **Generate a new API
   Key**, and paste it into `api_key`. The secret on the same tab signs OAuth
   1.0 requests and webhooks; this connection does not use it.
4. Open
   `https://trello.com/1/authorize?expiration=never&scope=read,write&response_type=token&key=`
   with the API key appended, check that it asks for read and write access,
   choose **Allow**, and paste the token from the page that follows into
   `token`. `expiration` also accepts `1hour`, `1day`, and `30days`; a shorter
   lifetime forces a rotation. The `account` scope is not needed, because the
   connector never reads email addresses.
5. To revoke the token, open `https://trello.com/u/{username}/account`, find the
   app under **Applications**, and choose **Revoke**; then authorize again and
   paste the new token. Trello answers a revoked or expired token with HTTP 401.

Trello documents that an API key may be public and that a token grants access to
the member's whole account. Dex Web stores both as secrets, because together
they authorize every request. Trello documents no format for either, so the
connector only checks that each fits inside the quoted header: printable ASCII
without spaces, quotes, commas, or backslashes. Credentials are reread before
every call, so a replacement takes effect without a restart. A 401 is never
resent, because a token cannot be refreshed.

### Trello OAuth is not in this release

Trello's three-legged OAuth 1.0 flow (`OAuthGetRequestToken`,
`OAuthAuthorizeToken`, `OAuthGetAccessToken`) signs every request with the app
secret; Dex Web and manifests support only OAuth 2.0 authorization-code
exchanges, so it does not fit the platform. Trello's newer OAuth 2.0 (3LO)
authorization at `auth.atlassian.com` was also deferred: its guide names two
different token URLs, its access token lasts one hour with a 90-day refresh
token, and the Dex Web exchange, scope-string check, and refresh rotation are
unverified for it. It can be added later as a second method with
`sdkgo/oauthtoken`.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/approved-request-card/main.go`](examples/approved-request-card/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := trello.NewProjectConnection(project, approvedrequestcard.ConnectionName)
if err != nil {
	return err
}
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../../sdkgo/projectconfig/README.md). The API key and
token stay in project storage and are read for every provider call; `endpoint`
and `maxResponseBytes` are startup configuration.

## Operations

Every operation bounds its requests by 25 seconds in total and each request by
20 seconds, below the 30-second Execute timeout and Trello's own 30-second
limit. Redirects are never followed. A response that contains the key or the
token is never returned. Reads and `updateCard` retry 408, 429, 5xx, and
transport failures; 404 selects `notFound` where the operation declares it;
400, 401, 403, 409, and other 4xx select `providerRejected`.

Trello limits each API key to 300 requests and each token to 100 requests per
10 seconds, and answers with 429 and a JSON code such as
`API_TOKEN_LIMIT_EXCEEDED`, which the Failure names. Trello documents no
`Retry-After`, so a 429 without one waits one 10-second window; a `Retry-After`
is honored when present.

Receipts carry the Call ID, the object ID, and Trello's `atl-request-id`
response header. Trello returned that header on every response observed while
building this connector, but does not document it, so it may be empty.

### listCards

Set exactly one of `BoardID` and `ListID`. `Status` is `open` (the default),
`closed` for archived cards, or `all`. `PageSize` is 1 to 100, and zero requests
50. The request sends `filter`, `fields`, `limit`, and `sort=-id`, which
Trello's nested-resource guide documents for a board's or a list's cards, so a
page holds the newest cards first. When a page is full, `NextBefore` is the
oldest card ID on it; pass it as `Before` to read older cards. Trello pages by
creation time, so a card created while you page appears only on a new first
page.

Trello's card list endpoints have no due-date filter, so the connector applies
`DueBefore` (strictly before), `DueAfter` (at or after), `HasDueDate`, and
`IsDueComplete` to each page. A page can therefore hold fewer matching cards
than `PageSize`, even none, while `NextBefore` is set; `ScannedCardCount` is
the number of cards Trello returned. Listed cards carry no description; read it
with `getCard`.

### getCard

`GetCardInput.CardID` is a card ID. The `Card` adds the plain-text
description of at most 16384 characters (Trello's documented limit), the list
name, and up to 50 members by name and username. Labels carry their ID, name,
and color. Trello's OpenAPI types `labels` and `idLabels` inconsistently, as
ID strings or label objects, so the connector accepts either and reports at
most 50 labels, with `HasMoreLabels` when a card carries more.

### createCard

`CreateCardInput` names the `ListID`, a one-line `Name`, and optionally a
plain-text `Description`, a `Due` instant, up to 50 `LabelIDs` and `MemberIDs`,
and a `Position` of `top`, `bottom`, or a positive number. The body is JSON,
which Trello documents as a replacement for its query parameters, so card
content never travels in a URL. Due dates are sent in UTC with millisecond
precision. Trello documents no limit for a card name or a comment; the
connector bounds both at 16384 characters, like a description.

Trello has no idempotency key, so the connector sends the request at most once
per Step execution:

| Outcome | Result |
| --- | --- |
| 2xx with a card ID | `created` |
| 400, 401, 403, 404, 409, or another 4xx except 408 and 429 | `providerRejected` |
| 429 | Retry, after `Retry-After` or one 10-second window |
| DNS, connect, or TLS failure before any connection opened | Retry |
| Timeout, dropped connection, 408, 3xx, or 5xx after dispatch | `uncertain` |
| 2xx whose body is oversized, unreadable, unusable, or reflects a credential | `uncertain` |
| An earlier attempt of the same Step execution recorded the dispatch checkpoint | `uncertain`, with nothing sent |

On `providerRejected` and `uncertain`, the Value echoes the requested name and
list without a card ID.

### updateCard

Every change is an absolute value: `ListID` (with `BoardID` for a list on
another board), `Position`, `IsClosed` (archive or reopen), `Due` or
`ShouldClearDue`, `IsDueComplete`, `LabelIDs` (a pointer to an empty list
removes every label), and `MemberIDs`. `AddLabelIDs` and `RemoveLabelIDs`
first read the card's labels and send the complete resulting set, so they
cannot be combined with `LabelIDs`.

Trello documents the `PUT` parameters but not whether `idLabels` and
`idMembers` replace the card's lists, and its default response fields omit
`dueComplete`. The operation therefore reads the card back after the `PUT` and
selects `updated` only when the card shows every requested value; otherwise it
selects `invalidResponse` naming the first missing change, such as `labels`.
`Position` is not compared, because Trello turns `top` and `bottom` into
numbers.

Because a repeated `PUT` of absolute values leaves the same card, every
ambiguous outcome, including a timeout after dispatch or a 5xx, retries instead
of selecting `uncertain`. A retry or backup attempt can move the card back to
the top of the list if someone reordered it in between, and an add or remove
can overwrite a label change another member made between the read and the
`PUT`.

### addComment

`AddCommentInput.Text` is plain text of at most 16384 characters, authored by
the token's member. Trello stores it as a `commentCard` action, whose ID is
`CommentID`. The branch table matches `createCard`, except that a 404 selects
`notFound`. A read-before-write over the card's comment actions would not make a
comment safe: two attempts both read before either writes, and it would also
suppress a deliberately repeated comment.

## Avoiding duplicate cards and comments

`createCard` and `addComment` use sync Execute durability although a request is
usually fast. With async durability, Dex runs a Step in a local phase of about
seven seconds and then dispatches a backup attempt, so a slow create would be
sent twice. A real Dex run against a fake Trello that held the create for nine
seconds recorded two create requests when the Step was overridden to async,
even with the heartbeat checkpoint, and one under sync;
`TestSlowCreateIsSentOnceUnderSyncDurabilityWithRealDex` and
`TestSlowCommentIsSentOnceUnderSyncDurabilityWithRealDex` guard this. Do not
override these Steps to async.

Before it sends, each of the two operations records a Dex heartbeat checkpoint
naming its Call ID. An attempt of the same Step execution that finds the
checkpoint, after a lost Worker or an Execute timeout, selects `uncertain`
without sending; `TestLostWorkerDuringCreateSelectsUncertainWithoutResendingWithRealDex`
force-stops the Worker while the fake holds the create and proves the next
Worker's attempt sends nothing. Only a 429 or a connection that never opened
clears the checkpoint and retries. Dex accepts the checkpoint when the Worker
writes it to its stream, before the request leaves; a Worker lost in that
instant, before Dex stored the checkpoint, could still send twice, and a crash
after the checkpoint but before the request left reports `uncertain` for a
request Trello never received.

`updateCard` keeps the async default. With a fake that held the `PUT` for nine
seconds, Dex dispatched a backup attempt, Trello received the same body twice,
and the card ended in the list with the labels and due date exactly as with one
request; `TestSlowMoveBackupAttemptLeavesTheSameCardWithRealDex` guards this.

An application handles `uncertain` without creating again automatically; the
[`approved-request-card`](examples/approved-request-card) example parks the
create for an operator, who confirms the card found on the board or approves a
new create.

## Not in this release

### Studio pickers

A board picker would list `GET /1/members/me/boards` and a list picker
`GET /1/boards/{id}/lists`. Both return a top-level JSON array, which Dex Web
rejects from a Studio command. A Studio command also injects one credential as
a bearer token or a raw header, and `Authorization` cannot be a raw-header
name, while Trello needs the key and the token together, in its `OAuth` header
or as two query parameters. Board and list IDs are therefore operation input.
To find them, run the documented calls with the same header:

```bash
curl -H 'Authorization: OAuth oauth_consumer_key="KEY", oauth_token="TOKEN"' \
  'https://api.trello.com/1/members/me/boards?fields=name&filter=open'
curl -H 'Authorization: OAuth oauth_consumer_key="KEY", oauth_token="TOKEN"' \
  'https://api.trello.com/1/boards/BOARD_ID/lists?fields=name&filter=open'
```

A board member can also export the board from the board menu with **Print,
Export, and Share**; every object in the JSON export carries its 24-character
`id`.

### Triggers

Trello webhooks (`POST /1/webhooks` with `callbackURL` and `idModel`) need
three things `sdkgo/webhooktrigger` does not offer yet:

- Trello sends an HTTP `HEAD` to the callback URL when the webhook is created
  and refuses to create it without a 200; `webhooktrigger.Endpoint` accepts only
  `POST` and has no handshake hook.
- Every delivery carries `X-Trello-Webhook`, a base64 HMAC-SHA1 of the body
  followed by the callback URL exactly as registered, keyed with the app secret
  from the **Trello Auth** tab. `VerifyRequest` would need that third secret and
  the registered callback URL as connection configuration.
- Registering the webhook is a provider write with a public HTTPS callback, and
  Trello deletes a webhook whose delivery receives HTTP 410 or whose token is
  revoked, so the binding needs a setup and renewal path.

Trello retries a failed delivery three times, after 30, 60, and 120 seconds,
and disables a webhook that fails for 30 days and over 1000 times. Its payload
carries `action.id`, which a source would use as the event ID, but Trello
shows it only in an example and does not call it a delivery ID. Until a source
exists, poll with `listCards` on the list or board.

## Example

[`examples/approved-request-card`](examples/approved-request-card) turns an
approved request into a card in the approved list: it pages through the board's
open cards for one that already carries the request ID, moves an existing one or
creates the card once, and comments with the approval. It uses all five
operations.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, RPC, persistence, duplicate-dispatch, and transition coverage:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/atlassian/trello/connector.yaml
go run ./cmd/connectorctl generate --check connectors/atlassian/trello/connector.yaml
```

The deterministic fakes cover the `OAuth` header and credentials kept out of
URLs, every branch above, page cursors and due filters, the dispatch checkpoint
and its clearing, JSON bodies, read-back verification of every update field,
label add and remove, plain-text errors without Trello text, rate-limit codes
with and without `Retry-After`, redirects, oversized and malformed responses,
credential reflection, and header-unsafe credentials.

No live Trello account was used. The following live behavior is unverified: the
app admin portal labels in the setup guidance; real response shapes of every
endpoint and field this connector requests; `sort=-id`, `limit`, and `before`
on a list's cards, which only the nested-resource guide documents; whether
`PUT /cards/{id}` replaces `idLabels` and `idMembers` and accepts `""` for
none; whether a JSON body works for every documented parameter; the card name
and comment length limits; whether a 429 is always returned before a write is
applied; whether 401 is also the status for a card the member cannot open;
`Retry-After` on 429; the `atl-request-id` header; and 429 and 5xx behavior
under load.
