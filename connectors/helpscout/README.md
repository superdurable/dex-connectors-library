# Help Scout Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Help Scout; no live Help Scout account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Help Scout connector connects a Dex application to the
[Help Scout Inbox API 2.0](https://developer.helpscout.com/mailbox-api/):

| Name | Kind | What it does | Happy branch |
| --- | --- | --- | --- |
| `searchConversations` | Query | One page of up to 25 conversations filtered by inbox, status, tag, customer email, and modification time | `searched` |
| `getConversation` | Query | One conversation with up to 25 of its newest threads | `found` |
| `replyToConversation` | Mutation | One customer reply, which Help Scout emails, or one internal note, sent at most once per Step execution | `replied` |
| `updateConversation` | Mutation | Sets the status or assignee and adds or removes tags; safe to repeat | `updated` |
| `findCustomerByEmail` | Query | Every customer profile with one email address | `found` |
| `conversationEvent` | Trigger | A signed conversation webhook, such as `convo.created` or `convo.customer.reply.created` | |

Every other branch is optional: `notFound`, `providerRejected`,
`invalidResponse`, and `defect`. `getConversation` adds `merged`;
`replyToConversation` has `uncertain` in place of `invalidResponse`;
`searchConversations` has no `notFound`. Every time in a Result is UTC.

## Statuses

The connector takes and returns Help Scout's own conversation statuses and
never maps them to another vocabulary:

| Status | Meaning |
| --- | --- |
| `active` | open and waiting for the team; a customer reply sets it |
| `pending` | open and waiting for the customer or a later action |
| `closed` | resolved; a customer reply reopens it |
| `spam` | marked as spam |

`helpscout.ConversationStatuses()` lists them. `searchConversations` also
accepts Help Scout's `all` filter, and a blank status lists `active`
conversations, Help Scout's default. Help Scout's `open` filter value is not
accepted, because v2 does not document what it lists. A Result passes through
any value Help Scout adds later; inputs are validated before any request.

## Authorization

A connection uses Help Scout's
[client credentials flow](https://developer.helpscout.com/mailbox-api/overview/authentication/),
which Help Scout documents for internal integrations with its own account.
Create an app at **Your Profile > My Apps > Create My App** in
<https://secure.helpscout.net/>; Help Scout requires a Redirection URL, and
any HTTPS placeholder works because this flow never redirects. Save the App ID
and App Secret in Dex Web's form as `app_id` and `app_secret`, and leave
`access_token` blank. The app acts with the permissions of the Help Scout user
who created it, and Help Scout apps have no scopes.

`access_token` is an output. `CredentialRefreshDriver` obtains it with
`oauthtoken.TokenEndpoint.ExchangeClientCredentials`, posting
`grant_type=client_credentials` with the App ID and App Secret in the form body
to `https://api.helpscout.net/v2/oauth2/token`, as Help Scout documents, and
records the expiry from `expires_in` (two days). The driver obtains a new token
when the stored one is absent or within five minutes of expiry; a token without
a recorded expiry is kept until Help Scout answers `401`. After a `401`, the
connector obtains a new token and repeats the request once. `invalid_client`
and `unauthorized_client` mean the App ID or App Secret must be replaced and
select `defect`; any other token failure returns Retry.

`webhook_secret` is optional and needed only by the Trigger: it is the secret
key of the Help Scout webhook, at most 40 characters, which you choose.

Help Scout's authorization-code flow, for apps that other Help Scout accounts
authorize, is not offered. Dex Web releases before `cli-v1.4.0` require every
manifest scope in the token response's `scope` string, and Help Scout defines
no scopes and returns none.
[superdurable/dex#581](https://github.com/superdurable/dex/pull/581) and
[#582](https://github.com/superdurable/dex/pull/582) let Dex Web accept such
providers from `cli-v1.4.0`, so a later connector release can add an
authorization-code method that requires it.

### Local connections

Build every local connection with `helpscout.NewLocalRenewingConnection`, as
[`examples/conversation-triage/main.go`](examples/conversation-triage/main.go)
does. It uses `localconfig.NewRefreshingCredentialProvider`, which stores the
obtained token and its expiry in the connection file Dex Web writes, under a
process-local lock, so restarts reuse it until it nears expiry.
`NewLocalConversationEventEndpointRunner` uses it too.

Code generation emits a refreshing provider only for OAuth manifests that map a
`refresh_token`. For this connector the generated `NewLocalConnection`, and
`NewLocalConversationEventTrigger` built on it, use
`localconfig.NewCredentialProvider`, which cannot store a token, so their Help
Scout calls return Retry, naming `NewLocalRenewingConnection`, until the Step
fails.

## Operations

### searchConversations

`searchConversations` reads one page of `GET /v2/conversations`, Help Scout's
page-numbered list, with each typed filter as a URL parameter: `mailbox`,
`status`, `tag`, and `modifiedSince`, which is sent in UTC whole seconds.
`customerEmail` becomes `query=(email:"...")`, which Help Scout combines with
the others by AND and documents as matching the address in To, Cc, or Bcc or
among a customer profile's emails. An address with a quote, parenthesis,
backslash, or display name selects `defect`, so it cannot change the query.
`BuildConversationSearchParameters` returns the parameters for review.

Help Scout returns 25 conversations per page, newest created first, without
threads. `page` is 1-based, at most 10,000; `nextPage` is set while Help Scout
links a next page. Help Scout added a cursor-paged `GET /v3/conversations` on
2026-09-23 with an exact `email` filter; this release keeps the v2 page list.

### getConversation

`getConversation` reads `GET /v2/conversations/{id}` and then the first page
of `GET /v2/conversations/{id}/threads`, which Help Scout sorts newest first.
`threadLimit` is 1 to 25, 10 by default, and `hasOlderThreads` reports that
more exist. Help Scout caps a conversation at 100 threads. Each thread body is
HTML, cut at 16 KiB (`helpscout.MaxThreadBodyBytes`) on a UTF-8 boundary and
flagged `isBodyTruncated`. A conversation merged in the last 60 days answers
`301`; the connector never follows it and selects `merged` with
`mergedIntoConversationId` from the `Location` header.

### replyToConversation and duplicate dispatch

`replyToConversation` posts `POST /v2/conversations/{id}/reply` with
`customer.id`, or `POST /v2/conversations/{id}/notes` when `isInternalNote` is
set. A reply without `customerId` reads the conversation first and uses its
primary customer. `status` optionally sets the conversation status with the
thread; otherwise a reply reactivates it. Help Scout answers `201` with the
new thread's ID in `Resource-Id`.

Help Scout documents no idempotency key, and threads carry no client-supplied
ID or metadata that a later attempt could find, so a read-before-write cannot
tell this Step's thread from an identical one, and it cannot see a thread
whose request is still in flight. The operation is therefore single-dispatch:

- it runs with sync durability, so Dex does not dispatch the Step a second
  time while a slow Help Scout answers;
- it records a Dex heartbeat checkpoint before the request leaves the Worker.
  A later attempt of the same Step execution that finds the checkpoint selects
  `uncertain` without sending;
- the request must answer three seconds before the attempt's deadline, at
  most the 30-second Execute timeout, and it is not sent when less than five
  seconds would remain. An application override to async durability therefore
  sends nothing in Dex's short local attempt;
- only a `429`, the `504` that Help Scout documents as an internal timeout that
  is safe to retry, or a request that never left the process returns Retry and
  clears the checkpoint. A `5xx` other than `504`, a `3xx`, a `408`, a timeout,
  or a lost answer after the request was written selects `uncertain`;
- a `404`, including a merged conversation, selects `notFound`, and any other
  `4xx`, such as `412` for a conversation locked at 100 threads or by age,
  selects `providerRejected`. Nothing was added.

The real-Dex tests in `mutation_dispatch_integration_test.go` prove it with a
fake Help Scout that answers after nine seconds: one POST under sync, one
POST under an async override, and an `uncertain` result without a second POST
when Dex retries the Step after the reply was sent.

### updateConversation

`updateConversation` reads the conversation, sends only the writes whose value
differs, and reads it back:

- a status with `PATCH /v2/conversations/{id}` and
  `{"op":"replace","path":"/status","value":"closed"}`;
- an assignee with `{"op":"replace","path":"/assignTo","value":456}`, or
  `isUnassigned` with `{"op":"remove","path":"/assignTo"}`. Users and teams
  share one ID space;
- tags with `PUT /v2/conversations/{id}/tags` and the complete list: the tags
  read, minus `removeTags`, plus `addTags`, compared without case.

Every write sets a final value, so the operation runs with async durability:
a second dispatch, or a retry after a lost answer, writes the same values
again or finds them applied and reports `wasAlreadyApplied`. The real-Dex test
lets Dex dispatch the Step again while a status write answers after nine
seconds, and the conversation ends with the status and one copy of the tag. A
tag that someone else changes between the read and the `PUT` is overwritten.

### findCustomerByEmail

`findCustomerByEmail` reads the first page of `GET /v3/customers?email=...`,
Help Scout's typed email filter, rather than the v2 `query=(email:"...")`
search, which Help Scout documents as matching emails that contain the
address. It returns up to 50 profiles, with `hasMore` when Help Scout has more,
and selects `notFound` when there are none. Help Scout can hold several
profiles for one person, so every match is returned and the Flow decides.

## Trigger

The `conversationEvent` Trigger serves one `webhooktrigger.Endpoint` per
connection. `Connection.ConversationEventWebhookHandler` returns it, and
`NewLocalConversationEventEndpointRunner` wraps every binding in a durable
inbox. For each delivery the endpoint:

1. accepts only `POST` up to `webhookMaxBodyBytes`, answering `405` or `413`;
2. verifies `X-HelpScout-Signature` as the base64
   [HMAC-SHA1](https://developer.helpscout.com/webhooks/) of the raw body with
   `webhook_secret`, comparing the decoded bytes in constant time; the test
   suite checks the signature Help Scout's documentation gives for its sample
   body. A failure answers `400`; a connection without a webhook secret
   answers `503`, so Help Scout retries. Verification needs only the secret:
   the endpoint's `EndpointConfig.CredentialRefresh` driver obtains a new
   token only for a record whose stored expiry has passed, which the local
   connection file otherwise refuses to return, so an absent or expiring token
   never delays a delivery. A record past its expiry while the token endpoint
   fails, or one whose App ID or App Secret Help Scout rejected, answers
   `503`;
3. decodes the 10 conversation events whose body is a v2 Conversation object,
   `helpscout.ConversationWebhookEvents()`, and acknowledges any other event,
   such as `customer.created` or `convo.deleted`, with `200`;
4. records the event for every binding whose `events` and `mailboxId` accept
   it, and answers `200` only after every record is on disk.

Help Scout sends no delivery ID, timestamp, or event time. The event ID is the
event, the conversation ID, and the first 32 hex digits of the body's
SHA-256, such as `convo.created:501:0f1e…`, so a redelivery of the same body
keeps its ID and a later event about the conversation has its own;
`OccurredAt` is when the endpoint received the request. Without a signed
timestamp, a captured delivery can be replayed, but it keeps its event ID and
starts no second Flow.

Help Scout retries a failed delivery up to 10 times, discards it, and
deactivates a webhook after 10 discarded events; a `410` deactivates it at
once, and the endpoint never answers `410`. Create the webhook without
Help Scout's `notification` flag, whose body names only a URL and answers
`400`. Both `V2` and `V3` payload versions decode.

The checked-in example wires the endpoint like this, from
[`examples/conversation-triage/main.go`](examples/conversation-triage/main.go):

```go
func newConversationEndpointRunner(
	store *localconfig.Store, client *dex.Client, flow *conversationtriage.Flow, logger *slog.Logger, connectionOptions []helpscout.Option,
) (*helpscout.ConversationEventEndpointRunner, error) {
	bindingLogger := logger.With("connector", helpscout.ConnectorID, "connection", conversationtriage.ConnectionName,
		"trigger", helpscout.ConversationEventTriggerDefinition.Trigger.TriggerName, "binding", conversationtriage.NewConversationTriggerBinding)
	return helpscout.NewLocalConversationEventEndpointRunner(store, conversationtriage.ConnectionName, []helpscout.LocalConversationEventTriggerRoute{{
		BindingName: conversationtriage.NewConversationTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, conversationtriage.AcceptNewConversation, conversationtriage.ResolveFlowID,
			conversationtriage.MapToFlowInput, sdkgo.WithTriggerLogger(bindingLogger)),
	}}, append(slices.Clone(connectionOptions), helpscout.WithLogger(logger))...)
}
```

One Trigger with an `events` filter, rather than one Trigger per event,
follows the Stripe and Calendly connectors: Help Scout sends every event of a
webhook to one URL with one secret, and one binding that accepts several
events delivers them in arrival order.

## Configuration UI

The `mailboxPicker` Studio unit pages `GET https://api.helpscout.net/v2/mailboxes`,
a fixed-host bearer command whose answer is a JSON object, and stores the
chosen inbox's numeric `mailboxId`. Dex Web reads the connection record for
each command and sends its stored `access_token`, so the list works once the
application has obtained and stored a token, on its first Help Scout call.
Before that, or after the token's two days while the application is stopped,
the command fails and the unit says so and falls back to its manual inbox ID
field. Studio commands cannot run the client credentials exchange themselves:
they send a stored credential as a bearer or header value and cannot post to
a token endpoint. The connection surface shows status; the App Secret, token,
and webhook secret stay in Dex Web's host form and never reach the frame. Dex
Web shows the connection **Expired** once the stored expiry passes while the
application is idle; the next Help Scout call renews it.

## Errors

A non-2xx answer never exposes Help Scout's `message` or `rejectedValue`. A
Failure repeats only up to five error `path` values with the error code from
their `about` link, such as `Help Scout rejected the request (HTTP 400) [status=EnumValue]`,
and the Receipt carries Help Scout's `logRef` for support.

| Answer | Result |
| --- | --- |
| 400, 413, 415, 422 | `providerRejected`, `VALIDATION` |
| 401 with a newly obtained token | `providerRejected`, `AUTHENTICATION` |
| 403 | `providerRejected`, `AUTHORIZATION`, such as an account without API access or payment |
| 404, 410 | `notFound` where declared, otherwise `providerRejected` |
| 409 | `providerRejected`, `CONFLICT` |
| 412, 423 | `providerRejected`, a locked conversation or customer |
| 301 | `merged` for `getConversation`, `notFound` for the Mutations |
| other 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed |
| 408, 429, 5xx, transport failure | Retry, after `X-RateLimit-Retry-After` or `Retry-After`; see the reply's exceptions above |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` |
| invalid input or credentials | `defect`, with no request |

## Security boundary

- The App Secret, access token, and webhook secret are `secretString`
  credentials. They never enter Flow input, Attributes, Results,
  receipts, logs, or HTTP responses.
- The connector sends a token only to `https://api.helpscout.net`, never
  follows a redirect, and reads at most `maxResponseBytes`.
- Failure messages are written by the connector from status codes and error
  tokens; Help Scout's message text is never read.

## Unverified live behavior

These rely on Help Scout's documentation fetched on 2026-09-30 and
2026-10-01, not on a live account:

- the token endpoint's error code and status for a wrong App Secret or a
  deleted app (the connector treats only `invalid_client` and
  `unauthorized_client` as final), and whether deleting the app revokes its
  tokens;
- whether a token obtained earlier stays valid after a new one is obtained,
  which matters when two processes renew at once;
- whether `GET /v2/mailboxes` accepts a client credentials token, as every
  other endpoint is documented to;
- whether the reply `text` is stored as given or escaped, and how line breaks
  render;
- whether a `504` on a reply truly never creates the thread, as documented;
- whether the v2 `tag` filter accepts one tag name exactly as `tag:"..."`
  search does, and whether `PATCH /status` accepts `spam`;
- whether the `/v3/customers` `email` filter is an exact match, and whether
  list results embed each customer's emails;
- whether a redelivered webhook carries the byte-identical body, which keeps
  its event ID;
- webhook bodies of `V3` payloads beyond the `system_user` person type.

## Verification

```bash
cd connectors/helpscout
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration run needs `dexcli dev` and takes about a minute, because the
duplicate-dispatch tests wait for a nine-second provider.
