# Microsoft Teams Connector

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
80 KB. Mentions, attachments, and cards are not supported in v0.1.0.

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
Graph's `error.code`, never `error.message`. A 401 refreshes the token and
retries once. 429, 503, 504, other 5xx, 408, and dropped connections retry
after `Retry-After` when Graph sends one. Teams allows about one request per
second per channel or chat.

## Duplicate-safe posting

Graph's `chatMessage` has no idempotency key and no client-settable ID, so a
repeated request posts a second message. Dex re-dispatches an async Step whose
local attempt passes about seven seconds and replays a Step after a lost
Worker, so every post:

1. runs with `sync` Execute durability, so no backup attempt runs while the
   first is in flight;
2. records a Dex heartbeat checkpoint before it sends;
3. on any later attempt of the same Step execution, never sends. A channel
   post reads the channel's 50 most recently active root messages, and a reply
   reads the thread's 50 newest replies, and reports `sent` with
   `isConfirmedByReadBack` when exactly one person's undeleted message created
   after the first send has the same subject and text. Otherwise it selects
   `uncertain`;
4. clears the checkpoint only when Graph provably stored nothing: a 429, or a
   connection that never opened.

An ambiguous response (timeout, 5xx, 408, an unreadable or oversized 2xx) keeps
the checkpoint and retries after at least five seconds so the read-back can see
the message. Reading back needs `ChannelMessage.Read.All`; without it the
attempt selects `uncertain`. The connection cannot read chat messages (that
needs `Chat.Read`), so an unconfirmed chat message selects `uncertain` at once.
A Flow must treat `uncertain` as "may have been posted" and never resend it
automatically.

The example's integration tests prove each step on a real Dex Server: a fake
that answers posts after nine seconds receives one post per Step, a lost
response is found by the read-back without a second post, and a Worker killed
mid-post is replaced without posting again. With an application override to
async durability, the same nine-second fake received two posts.

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

## Local configuration

The Worker reads a Dex Web connection file. A hand-written record looks like
this; Dex Web writes the real tokens and `credentialExpiresAt` after Connect:

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "microsoft-teams",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/microsoft/teams",
    "moduleVersion": "v0.1.0",
    "provider": "microsoft",
    "connectionName": "microsoft-teams",
    "configuration": {},
    "credentials": {
      "oauth_client_id": "00001111-aaaa-2222-bbbb-3333cccc4444",
      "oauth_client_secret": "<client secret value>",
      "access_token": "<from OAuth>",
      "refresh_token": "<from OAuth>"
    }
  }]
}
```

## Example

[`examples/incident-acknowledgement`](examples/incident-acknowledgement) posts
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
