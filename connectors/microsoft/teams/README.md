# Microsoft Teams Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/microsoft/teams/go@v0.24.0`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/microsoft/teams`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Microsoft Graph; no Microsoft tenant was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

This module posts Microsoft Teams channel messages, thread replies, and chat
messages, and reads the replies to a channel message, through Microsoft Graph
v1.0 with delegated OAuth. A Flow can post an update, ask for an answer in the
thread, and wait on a durable Timer until a person replies.

Every message is posted as the signed-in work or school account. Graph allows
application (app-only) permissions for channel posts only to migrate messages
into Teams, so this connector has no app-only method. Use a dedicated account
when the sender should not be a person.

## Operations

| Operation | Graph request | Kind | Permission |
| --- | --- | --- | --- |
| `PostChannelMessage` | `POST /teams/{team-id}/channels/{channel-id}/messages` | Mutation | `ChannelMessage.Send` |
| `PostThreadReply` | `POST /teams/{team-id}/channels/{channel-id}/messages/{message-id}/replies` | Mutation | `ChannelMessage.Send` |
| `ListThreadReplies` | `GET /teams/{team-id}/channels/{channel-id}/messages/{message-id}/replies` | Query | `ChannelMessage.Read.All` |
| `PostChatMessage` | `POST /chats/{chat-id}/messages` | Mutation | `ChatMessage.Send` |

The names follow the Slack connector's `PostChannelMessage` and
`PostThreadReply`. Posts accept `text` or `html` content, an optional
`importance`, and, for a root channel message, a one-line `subject`. The
connection's `maxMessageBytes` bounds content and subject before anything is
sent; Microsoft's limit is about 100 KB per post and it advises staying within
80 KB. Mentions, attachments, and cards are not supported.

`ListThreadReplies` returns one page of 1 to 50 replies and a `nextCursor`.
Graph accepts only `$top` on this list, so no `$select` is sent; it documents
no order and returns the newest reply first in its examples. Each reply carries
its ID, sender, creation and edit times, deletion flag, and `text`: HTML is
reduced to visible text, mentions keep the person's name, emoji keep their
character, and block elements become line breaks. A next-page link is followed
only when it stays on the endpoint's scheme and host and names the same thread.

Every operation selects `defect` for invalid input or configuration without a
request. A conclusive Graph refusal selects `providerRejected` (and `notFound`
for a missing thread when reading); the failure names the HTTP status and
Graph's `error.code`, never `error.message`. Reads may refresh a rejected
token and retry once. A post refreshes a rejected token when supported and
records permission for the next Dex attempt to resend only when refresh returns
a different valid token; otherwise 401 selects `providerRejected`. A credential
failure requiring reauthorization selects `defect` with a reconnect hint after
recording `notSent`; if an earlier post may have been sent, it selects
`uncertain` with the reconnect hint. A transient credential failure retries. A
429 or 503 on a post records permission for the immediately following attempt
to retry; 409 and other ambiguous write responses are reconciled or select
`uncertain`. Reads retry transient failures after `Retry-After` when Graph
sends one. Teams allows about one request per second per channel or chat.

## Duplicate-safe posting

The documented [channel](https://learn.microsoft.com/graph/api/channel-post-messages)
and [chat](https://learn.microsoft.com/graph/api/chat-post-messages) create
requests expose no idempotency key or client-settable message ID. A repeated
request can post a second message. Dex can replay a Step after Worker loss
or a result lost before commit, so every post:

1. runs with `sync` Execute durability so its heartbeat is visible to later attempts;
2. uses the Connector SDK single-dispatch fence to claim the write before it sends;
3. sends again only on the immediately following attempt after a recorded
   `notSent` or `canResend` checkpoint. Graph's 401 after a successful token
   refresh, 429 and 503 responses, and a traced connection failure before any
   connection opened, permit a retry;
4. after an ambiguous send, reads a known message ID when possible. Without
   one, a channel post searches the 50 most recently active root messages and
   a reply searches the thread's 50 newest replies. Exactly one matching
   undeleted message may confirm the post; no match or several matches select
   `uncertain`.

An ambiguous response (timeout, 408, 409, 500, 504, an unreadable or oversized
2xx) keeps the claim and retries after at least five seconds so the read-back can see
the message. Reading back needs `ChannelMessage.Read.All`; without it the
attempt selects `uncertain`. The connection cannot read chat messages (that
needs [delegated `Chat.Read`](https://learn.microsoft.com/graph/api/chatmessage-get)),
so an unconfirmed chat message selects `uncertain` at once.
A Flow must treat `uncertain` as "may have been posted" and never resend it
automatically.

[Microsoft Graph's error reference](https://learn.microsoft.com/graph/errors)
defines 401 as missing or invalid authentication. We infer that authentication
rejection happens before Graph processes the write; this has not been verified
against a live Teams tenant. A refresh must produce a different valid token
before the next attempt can resend. The same reference documents that 409 can
occur for a request conflicting with current state, so the connector reads back
instead of treating it as a conclusive rejection.
[Microsoft Graph's throttling guidance](https://learn.microsoft.com/graph/throttling)
documents that 429 requests fail before processing. Its
[error reference](https://learn.microsoft.com/graph/errors) recommends retrying
503 after a delay. The exact 503 no-acceptance behavior for Teams messages and
the completeness of the recent-message read-back remain unverified against a
live tenant; an inconclusive read-back never permits another write.
An in-flight Step from an older connector release carries a legacy heartbeat
marker that this fence cannot read. Its next attempt fails closed and needs
operator reconciliation; upgrade while no post Step is in flight when possible.

## Mocks

`teamsmock` provides one consistent incident world: the default channel post
creates the root message, the default reply posts status in its thread, the
default read returns the bot's status reply and Ada's acknowledgement, and the
default chat post is an escalation to the on-call chat. Each operation also
has a conclusive `providerRejected` mock; the read has `notFound` and
`invalidResponse` cases.
The runnable example uses the generated connection in a real-Dex test:

```go
	mock := teamsmock.New(t, ConnectionName)
	policy := AcknowledgementPolicy{CheckInterval: time.Second, MaximumChecks: 3}
	flow := NewFlow(mock.Connection(), ChannelSelection{TeamID: integrationTeamID, ChannelID: integrationChannelID}, EscalationChatSelection{}, &policy)
```

The example's real-Dex tests prove that a delayed post completes once, a lost
response is found by read-back, and a Worker lost mid-post is replaced without
posting again.

## Microsoft Entra app registration

Microsoft OAuth needs Dex CLI 1.4.1 or later. Microsoft does not echo
`offline_access` in the token response's `scope`; Dex Web 1.4.1 accepts the
returned refresh token as proof of it, and earlier releases fail the callback.

1. In the [Microsoft Entra admin center](https://entra.microsoft.com), open
   **Entra ID > App registrations > New registration**. Choose **Accounts in
   any organizational directory (multitenant)**: manifest OAuth endpoints are
   static, so Dex uses
   `https://login.microsoftonline.com/organizations/oauth2/v2.0/authorize` and
   `.../token`, which reject a single-tenant app with `AADSTS50194`. Add a
   **Web** redirect URI with the exact value Dex Web shows.
2. Copy **Application (client) ID**. Under **Certificates & secrets > Client
   secrets > New client secret**, copy the secret's **Value** (shown once).
3. Under **API permissions > Add a permission > Microsoft Graph > Delegated
   permissions**, add the permissions below.
4. Have an administrator grant consent for `ChannelMessage.Read.All`.

| Delegated permission | Used for | Administrator consent |
| --- | --- | --- |
| `Team.ReadBasic.All` | team picker (`GET /me/joinedTeams`) | not required |
| `Channel.ReadBasic.All` | channel picker (`GET /teams/{id}/channels`) | not required |
| `ChannelMessage.Send` | channel posts and thread replies | not required |
| `ChannelMessage.Read.All` | `ListThreadReplies` and post read-backs | **required** |
| `ChatMessage.Send` | `PostChatMessage` | not required |
| `Chat.ReadBasic` | chat picker (`GET /me/chats?$expand=members`) | not required |
| `offline_access` | refresh token | not required |

The consent column comes from Microsoft's
[permissions reference](https://learn.microsoft.com/graph/permissions-reference).
A tenant can also turn user consent off, which makes every permission need an
administrator. Because Dex requests all permissions together, a non-admin sees
**Need admin approval** until an administrator chooses **Grant admin consent**
on the app's API permissions page (or signs in once and checks **Consent on
behalf of your organization**). Dex requests no `openid`, `profile`, or `email`.

Personal Microsoft accounts are not supported: Graph's Teams messaging APIs do
not support them. National clouds are not supported because sign-in always
uses `login.microsoftonline.com`.

Dex Web stores the access and refresh tokens. The connector refreshes the
access token at the organizations token endpoint five minutes before it
expires, without a `scope` parameter, and keeps the replacement refresh token
Microsoft returns. `invalid_grant`, `invalid_client`, `unauthorized_client`,
`interaction_required`, and `consent_required` mark the connection for
reauthorization; other errors and every 5xx are retried. The refresh never
requires `offline_access` in the returned scope, and a refresh that returns
fewer permissions is kept: the operation needing a missing permission reports
Graph's 403.

## Pickers

Dex Web shows three units backed by read-only Graph `GET` commands that send
the access token as a bearer credential:

- `teamPicker` lists `GET /me/joinedTeams` and stores `teamId` and `teamName`.
  It lists only teams the account directly belongs to.
- `channelPicker` takes the saved `teamId`, lists
  `GET /teams/{teamId}/channels?$select=id,displayName,membershipType`, follows
  `$skiptoken`, and stores `channelId` and `channelName`. Private and shared
  channels appear only when the account is a member.
- `chatPicker` lists `GET /me/chats?$expand=members&$top=50`, labels each chat
  by its topic or member names, and stores `chatId` and `chatName`.

Each unit also accepts a typed ID and explains where Teams shows it.

## Not supported: Triggers and Adaptive Card approvals

There are no Triggers. A change-notification subscription on
`/teams/{team-id}/channels/{channel-id}/messages` works with the delegated
`ChannelMessage.Read.All` this connection already holds, but a Trigger would
need three things the SDK lacks: answering Graph's `validationToken` handshake
on the notification URL (`sdkgo/webhooktrigger` has no handshake hook yet), a
lifecycle notification endpoint for any subscription longer than one hour, and
a durable renewal loop before each subscription expires. Tenant-wide
`/teams/getAllMessages` needs application permissions. Polling with delta
queries would instead need a durable per-binding cursor that `sdkgo` does not
have. Until then a Flow waits for an answer by reading replies on a Timer, as
the example does.

There is no Adaptive Card approval. Graph can post a card, but the
`Action.Submit` or `Action.Execute` that a button press sends goes only to a
Bot Framework bot registered for the app, which this delegated connector is
not. The example asks people to reply with a phrase instead.

## Project connection

Dex Web saves the client ID and secret and, after Connect, the access and
refresh tokens in encrypted project storage. Load the project configuration
once at application startup and open the connection by the name its operations
use, as
[`examples/incident-acknowledgement/main.go`](go/examples/incident-acknowledgement/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := teams.NewProjectConnection(project, incidentacknowledgement.ConnectionName)
```

`projectconfig.LoadFromEnvironment` reads the `DEX_PROJECT_*` configuration
that Dex Web or Superverse Studio writes; see
[`sdkgo/projectconfig`](../../../sdkgo/projectconfig/README.md#application-loading).
Set the same `ConnectionName` beside the typed `Connection` in each operation:
a Step whose `ConnectionName` is empty or differs from its connection's name
panics at construction.

## Example

[`examples/incident-acknowledgement`](go/examples/incident-acknowledgement) posts
an incident update, replies in its thread, reads the replies until a person
acknowledges, and escalates to a chat when nobody does. It uses every
operation and all three pickers.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ../../../sdk/react && npm run build --prefix ../../../sdk/react
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

With the latest Dex development server running, the example owns the real
Worker, retry, Timer, and duplicate-dispatch coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

Unverified live behavior: no call has reached Microsoft Graph or the Microsoft
identity platform. Unverified are the OAuth consent and code exchange in Dex
Web, the exact `scope` string Microsoft returns, refresh and refresh-token
rotation, admin-consent behavior, Graph's error codes and response shapes,
whether Teams stores a text post as text or HTML (the read-back compares
visible text either way), reply order, `$skiptoken` paging, the chat picker's
`$expand=members` under `Chat.ReadBasic`, and Dex Web's percent-encoding of the
`$select`, `$expand`, `$top`, and `$skiptoken` query names.
