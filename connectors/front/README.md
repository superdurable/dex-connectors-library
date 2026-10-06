# Front Connector

> **Verification status: partial live.** Only placeholder-token requests reached Front, and all returned 401; everything else ran on a real Dex stack against a local stand-in. No live Front account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Front connector connects a Dex application to the
[Front Core API](https://dev.frontapp.com/reference/introduction):

| Name | Kind | What it does | Happy branch |
| --- | --- | --- | --- |
| `searchConversations` | Query | One page of up to 100 conversations by text, inbox, tag, status filters, recipient email, assignee, and recent activity | `searched` |
| `getConversation` | Query | One conversation with up to 25 of its newest messages and 25 newest internal comments | `found` |
| `replyToConversation` | Mutation | One reply that Front delivers to the recipients, or one internal comment, sent behind a dispatch checkpoint that narrows but does not close the duplicate window | `replied` |
| `updateConversation` | Mutation | Sets the status or ticket status and the assignee and adds or removes tags; safe to repeat | `updated` |
| `findContactByEmail` | Query | The contact whose email handle equals one address | `found` |

Every other branch is optional: `notFound`, `providerRejected`,
`invalidResponse`, and `defect`. `getConversation` adds `merged`;
`replyToConversation` has `uncertain` in place of `invalidResponse`;
`searchConversations` has no `notFound`. Every time in a Result is UTC.

The operations mirror the Help Scout, Intercom, and Zendesk desks: the same
operation and branch names, `isInternalNote` to choose an internal comment over
a customer reply, a contact lookup that returns a list, and a read-back update.
A Flow that swaps desks changes its connection and its status values, never
its branch handling.

## Statuses

The connector takes and returns Front's own values and never maps them to
another vocabulary. Front uses three vocabularies, and the connector keeps
them apart:

| Where | Values | Meaning |
| --- | --- | --- |
| `Conversation.Status`, reported | `unassigned`, `assigned`, `archived`, `deleted` | open without and with an assignee, archived (or snoozed), and in the trash |
| `UpdateConversationInput.Status`, written | `open`, `archived`, `deleted` | `open` reads back as `assigned` or `unassigned` |
| `SearchConversationsInput.Statuses`, the `is:` filter | `open`, `archived`, `snoozed`, `trashed`, `assigned`, `unassigned`, `unreplied`, `waiting`, `resolved` | combined with AND, such as `open` with `unassigned` |

Front reports a snoozed conversation as `archived` with a scheduled reminder;
`Conversation.IsSnoozed` reports that case. A company that uses ticketing also
has custom ticket statuses: `Conversation.StatusID`, such as `sts_5x`, names
one, and `StatusCategory` is its category, `open`, `waiting`, or `resolved`.
`updateConversation` sets one with `StatusID`, never together with `Status`.
`front.ConversationStatuses()`, `front.ConversationStatusChanges()`, and
`front.SearchStatusFilters()` list the values. Front's `spam` write status is
not offered, because no reported status could confirm it on read-back. A
Result passes through any value Front adds later; inputs are validated before
any request.

The ticket fields a Flow uses for service levels are `WaitingSince`, the time
of the oldest unreplied message, `StatusCategory`, `TicketIDs`, and a task's
`DueAt`. Front's SLA rules themselves are not exposed by the Core API.

## Authorization

A connection uses one Front
[API token](https://dev.frontapp.com/docs/create-and-revoke-api-tokens)
(`front-api-token`), sent as `Authorization: Bearer` only to
`https://api2.frontapp.com`. A company admin creates it at
<https://app.frontapp.com/> under **Settings > Developers > API Tokens >
Create API token** with:

- the **Access resources** feature;
- the **Shared resources** namespace with **All shared workspaces**, and
  **Global resources** for company tags and teammates; **Private resources**
  only when Flows must reach teammates' personal inboxes;
- read on conversations, messages, comments, contacts, inboxes, tags, and
  teammates, write on conversations and comments, and send on messages.

Front API tokens apply to the whole company and do not expire. Deleting one
stops every request at once; paste a new token into the connection, and the
running Worker uses it on its next call. Credentials are reread before every
call; the response limit is startup configuration.

### Why not OAuth

Front's OAuth, at `https://app.frontapp.com/oauth/authorize` and
`https://app.frontapp.com/oauth/token`, is not declared, because Dex Web
cannot complete its authorization today:

- Front's token endpoint requires the client as HTTP Basic credentials. Dex
  Web's authorization-code exchange sends `client_id` and `client_secret` in a
  form body, and the manifest schema cannot select another client
  authentication, as the Airtable and Notion connectors also report.
- Front's token response carries `expires_at` instead of `expires_in`, so
  neither Dex Web nor `sdkgo/oauthtoken` reads a lifetime from it.
- Front sets scopes on the app, not in the authorization request, while the
  manifest schema requires at least one scope.
- Only Front admins can authorize an OAuth app.

The access token lasts an hour, and the refresh token six months; Front
returns the same refresh token until its last 24 hours, then a new one. A
later release can add an OAuth method that refreshes with
`oauthtoken.ClientSecretBasic` once Dex Web can complete the code exchange.

## Project configuration

Dex Web or Superverse Studio writes this connection record to the project
configuration for the connection name the application uses:

```json
{
  "connectorId": "front",
  "connectionName": "front-support",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/front",
  "provider": "front",
  "configuration": {}
}
```

The `api_token` credential stays in the private project storage and is
resolved for every call. The application reads the configuration through the
`DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading):
it loads it with `projectconfig.LoadFromEnvironment`, opens the connection with
`front.NewProjectConnection`, and loads each Step's picker values once, as
[`examples/conversation-triage/main.go`](examples/conversation-triage/main.go)
does:

```go
// loadRoutingConfiguration treats unsaved pickers as empty, which fails each Flow with guidance instead of the Worker.
func loadRoutingConfiguration(
	configuration projectconfig.Configuration, logger *slog.Logger,
) (sdkgo.ConnectorLoadedConfiguration[conversationtriage.RoutingConfiguration], error) {
	reference := conversationtriage.RoutingConfigurationRef()
	loaded, err := provider.LoadOperationConfiguration[conversationtriage.RoutingConfiguration](configuration, reference)
	if errors.Is(err, projectconfig.ErrObjectNotFound) {
		logger.Warn("the triage tag is not configured; choose it in Dex Web and restart", "step", reference.StepType)
		return sdkgo.ConnectorLoadedConfiguration[conversationtriage.RoutingConfiguration]{Reference: reference}, nil
	}
	return loaded, err
}
```

## Operations

### searchConversations

`searchConversations` reads one page of
[`GET /conversations/search/{query}`](https://dev.frontapp.com/docs/search-1).
Each typed field becomes one Front filter, and Front combines them with AND:
`inboxId` as `inbox:`, `tagId` as `tag:`, each of `statuses` as `is:`,
`recipientEmail` as `recipient:`, which matches a from, to, cc, or bcc handle of
any message, `assigneeId` as `assignee:`, and `activityAfter` as `after:` in
Unix seconds, which matches a message or comment created after that time.
`text` is Front search text, words that must all match and quoted phrases, and
is sent unchanged, so Front also applies a filter written in it; quote text
that comes from a customer. A recipient address with a display name, quote,
space, or parenthesis selects `defect`, so it cannot add a filter. The typed
fields add at most 14 filters, within Front's limit of 15.
`front.BuildConversationSearchQuery` returns the query, and the Result's
`query` repeats it; the query travels as one percent-encoded path segment.

Front orders results by last activity, newest first, and returns `_total`
as `totalCount`. `pageSize` is 1 to 100, 25 by default. `nextPageToken` is
the `page_token` of Front's `_pagination.next` link, which may name the
company's own host, such as `https://acme.api.frontapp.com`. The connector
never follows that link: it checks that the link uses HTTPS on
`api2.frontapp.com` or a `*.api.frontapp.com` host, names the same resource,
and carries a token, and otherwise selects `invalidResponse`. The next request
sends the token to `https://api2.frontapp.com` with the same filters. Front
can return fewer conversations than `pageSize` while more remain. Front limits
search to 40 percent of the company's rate limit.

### getConversation

`getConversation` reads `GET /conversations/{id}`, the first page of
`GET /conversations/{id}/messages` with `limit` set to `messageLimit`, and
`GET /conversations/{id}/comments`, which Front lists in full; each list is
newest first. `messageLimit` and `commentLimit` are 1 to 25, 10 by default,
and `hasOlderMessages` and `hasOlderComments` report that more exist. Front
has no limit for the comment list, so the whole list counts against
`maxResponseBytes`: a conversation whose comments exceed it selects
`invalidResponse` whatever `commentLimit` is. Message
bodies, HTML for email, and comment bodies are cut at 16 KiB
(`front.MaxBodyBytes`) on a UTF-8 boundary and flagged `isBodyTruncated`.
Each message lists its recipients with their roles; the `from` handle of the
newest inbound message is the requester, and `contactId` names its Front
contact when one exists. A merged conversation answers `301`; the connector
never follows it and selects `merged` with `mergedIntoConversationId` from the
`Location` header.

### replyToConversation and duplicate dispatch

`replyToConversation` posts `POST /conversations/{id}/messages`, which Front
delivers on the conversation's channel to its recipients, or
`POST /conversations/{id}/comments` when `isInternalNote` is set. A reply
sends `text` unchanged as Front's `text` and as an HTML `body` with one
escaped paragraph per blank-line-separated block (`front.BuildReplyHTML`). It
sends `options.archive` as `shouldArchive`, false by default, so a reply keeps
the conversation open, unlike Front's own default of archiving it. `authorId`
names the teammate the reply is sent for or who writes the comment. A comment
sends `text` unchanged; Front renders Markdown in comments.

Front accepts a reply with `202` and a `message_uid`, which the Result returns
as `messageUid`; `GET /messages/alt:uid:{uid}` reads the message once Front
has created it, and a channel can still fail to deliver it later. A comment
answers `201`, and the Result returns its `commentId`.

Front documents no idempotency key, and messages and comments carry no
client-supplied ID that a later attempt could find, so the operation is
single-dispatch:

- it runs with sync durability, so Dex does not dispatch the Step a second
  time while a slow Front answers;
- it records a Dex heartbeat checkpoint before the request leaves the Worker.
  A later attempt of the same Step execution that finds the checkpoint selects
  `uncertain` without sending;
- the request must answer three seconds before the attempt's deadline, at
  most the 30-second Execute timeout, and it is not sent when less than five
  seconds would remain;
- only a `429`, which Front answers before applying the request, or a
  connection that provably never opened, a failed DNS lookup, connect, or TLS
  handshake with no connection obtained, returns Retry and clears the
  checkpoint. A `5xx`, a `408`, a timeout, or any other transport failure
  selects `uncertain`, so an HTTP client whose transport drops the request's
  trace cannot turn a lost answer into a second send;
- a `404`, or a `301` for a merged conversation, selects `notFound`, and any
  other `4xx` selects `providerRejected`. Nothing was added.

The checkpoint narrows the duplicate window but does not close it. Dex accepts
the checkpoint when the Worker writes it to its stream, before the request
leaves; a Worker lost in that instant, before Dex stored the checkpoint, could
still send twice, and a crash after the checkpoint but before the request left
reports `uncertain` for a request Front never received.

The real-Dex tests in
[`examples/conversation-triage`](examples/conversation-triage/main_integration_test.go)
prove it with a fake Front that answers after nine seconds, one comment POST
under sync, and with a Worker lost while Front holds the comment: the attempt
on the new Worker selects `uncertain` without a second POST.

### updateConversation

`updateConversation` reads the conversation, sends only the writes whose value
differs, and reads it back:

- `status` or `statusId`, and `assigneeId` or `isUnassigned`, in one
  `PATCH /conversations/{id}`; unassigning sends `"assignee_id": null`;
- `addTagIds` with `POST /conversations/{id}/tags` and `removeTagIds` with
  `DELETE /conversations/{id}/tags`, each with only the tags still to change.
  Front's tag endpoints add and remove a set, so a concurrent tag change by
  someone else is kept.

Every write sets a final value, so the operation runs with async durability: a
second dispatch, or a retry after a lost answer or a server error, writes the
same values again or finds them applied and reports `wasAlreadyApplied`. When
the read-back does not yet show a change Front accepted, the attempt returns
Retry, which repeats the writes safely. A `4xx` on a write selects
`providerRejected`, including a `404` or `410`, because the read just found the
conversation, so the missing resource is most likely a teammate, tag, or
ticket status; a `301` on a write selects `notFound` for a conversation merged
since the read. When an earlier write of the same attempt succeeded, the
Failure message says that it stays applied. The real-Dex test lets
Dex dispatch the Step again while a tag write answers after nine seconds; the
conversation ends with the assignee and one copy of the tag.

The writes converge for one Step, but they are not ordered against later
Steps: a write from a duplicate or retried attempt that Front holds can land
after the Step completes and undo a later Step's change to the same field,
such as a later unassignment or tag removal. `wasAlreadyApplied` means this
attempt wrote nothing; an earlier attempt may have.

### findContactByEmail

`findContactByEmail` reads `GET /contacts/alt:email:{email}`, Front's contact
alias. Front keeps each handle on at most one contact, so `contacts` holds one
contact on `found`, with its handles and list names, and none on `notFound`.
A returned contact that does not hold the email handle, compared without case,
selects `invalidResponse`. The other desks return every matching profile; the
list keeps the shape the same.

## Triggers are not in this release

Front pushes events in two ways, and neither fits `sdkgo/webhooktrigger` yet:

- [Application webhooks](https://dev.frontapp.com/docs/application-webhooks)
  sign each event with `X-Front-Signature`, the base64 HMAC-SHA256 of
  `X-Front-Request-Timestamp`, a colon, and the raw body, keyed by the app's
  signing key, and retry a failed delivery up to three times. Saving the
  webhook URL first sends a signed `sync` request with an `X-Front-Challenge`
  header, which the endpoint must answer within 10 seconds by echoing the
  challenge in its body. `webhooktrigger.Endpoint` has no handshake hook and
  answers `200` without a body, so Front would refuse the URL.
- [Rule webhooks](https://dev.frontapp.com/docs/rule-webhooks) need no
  handshake and sign the body with HMAC-SHA1 and the Webhooks app's API
  secret, but Front never retries them. The endpoint answers `503` whenever it
  cannot record an event, such as while no binding runs, expecting the
  provider to retry, so a rule webhook would silently lose those events.

A conversation Trigger needs a handshake hook in `webhooktrigger` that answers
the application webhook's challenge after verifying its signature. Polling
Front's `GET /events` instead needs a durable per-binding cursor, which `sdkgo`
does not have. Until then, start a Flow per conversation from the
application, as the example does.

## Configuration UI

The Studio bundle in [`ui/`](ui) offers three units, each listing live through
a fixed-host bearer command whose answer is a JSON object, with a manual ID
field when the list cannot load:

| Unit | Command | Stores |
| --- | --- | --- |
| `inboxPicker` | `GET https://api2.frontapp.com/inboxes` | `inboxId`, such as `inb_41w25` |
| `teammatePicker` | `GET https://api2.frontapp.com/teammates` | `teammateId`, such as `tea_2thf` |
| `tagPicker` | `GET https://api2.frontapp.com/tags?limit=100`, following `page_token` | `tagId`, such as `tag_13o8r1` |

Dex Web sends the connection's stored `api_token` with each command, so the
lists work as soon as the connection is saved; the token never reaches the
frame. The tag picker follows Front's `_pagination.next` only for a link on
Front's API hosts and stops after 20 pages. Front's inbox and teammate lists
are not paginated. The connection surface shows status only.

## Errors

A non-2xx answer never exposes Front's `_error` title or message: the
Failure names only the HTTP status.

| Answer | Result |
| --- | --- |
| 400, 413, 422 | `providerRejected`, `VALIDATION` |
| 401 | `providerRejected`, `AUTHENTICATION`, such as a deleted token |
| 403 | `providerRejected`, `AUTHORIZATION`, such as a token without the namespace or permission |
| 404, 410 | `notFound` where declared, otherwise `providerRejected`; `providerRejected` on an `updateConversation` write after its read found the conversation |
| 409 | `providerRejected`, `CONFLICT` |
| 301 | `merged` for `getConversation`, `notFound` for the Mutations, naming the absorbing conversation |
| other 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 429 | Retry after `Retry-After` seconds, capped at an hour; without it, at the `X-Ratelimit-Reset` second, one second to a minute later |
| 408, 5xx, transport failure | Retry, except `uncertain` for a sent reply or comment |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` |
| invalid input or credentials | `defect`, with no request |

Front's rate limit is per company: 50, 100, or 200 requests a minute on the
Starter, Professional, and Enterprise plans, plus a burst allowance. Per
conversation, the `PATCH` and assignee writes share a guard of five requests a
second, and so do the tag writes. `getConversation` sends three requests and
`updateConversation` up to five, so size Flow concurrency to the plan.

## Security boundary

- The API token is a `secretString` credential. It never enters Flow input,
  Attributes, Results, receipts, logs, or the Studio frame.
- The connector sends the token only to `https://api2.frontapp.com`, never
  follows a redirect or a next-page link, and reads at most
  `maxResponseBytes`; an answer that reflects the token selects
  `invalidResponse`.
- Failure messages are written by the connector from status codes; Front's
  message text is never read.

## Unverified live behavior

On 2026-10-04, requests with a placeholder token to `GET /tags`, `GET /me`,
`GET /conversations/search/is%3Aopen`, and
`GET /contacts/alt:email:jane@acme.example.com` on `https://api2.frontapp.com`
each answered `401` with `{"_error":{"status":401,"title":"Unauthenticated","message":"Invalid token"}}`,
which the connector maps to `providerRejected` without the message, and the
Studio `listTags` and `listInboxes` commands, run through Dex Web with the
same placeholder, failed with `CONNECTOR_PROVIDER_COMMAND_FAILED` without
reflecting it. Everything else relies on Front's documentation and OpenAPI
description fetched that day, not on a live account:

- whether a `page_token` from a company-host `next` link works on
  `api2.frontapp.com`, as the base-URL guide says either host does;
- whether one `PATCH` that carries both `status` and `assignee_id` applies
  both, as the schema allows;
- whether `GET /conversations/{id}` shows a `PATCH` or tag change at once;
  the connector retries until it does;
- which status Front answers for a write that names an unknown teammate,
  tag, or ticket status; its reference documents only `204`, `301`, and `400`;
- whether a reply with an API token and no `author_id` is accepted, and on
  which channel Front sends it;
- whether a `5xx` on a reply or comment ever leaves it created, which the
  connector assumes;
- whether `alt:email:` matches an address in another letter case;
- every answer to a valid token, including whether `Retry-After` accompanies
  every `429`;
- whether `GET /conversations/{id}/comments` returns every comment in one
  list, as its reference shows no pagination;
- whether a page token expires, and the status Front answers then;
- the inbox, teammate, and tag pickers against a real company.

## Verification

```bash
cd connectors/front
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration run needs `dexcli dev` and takes about 30 seconds, because
the duplicate-dispatch tests wait for a nine-second provider.
