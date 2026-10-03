# Mailchimp Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Mailchimp; no live Mailchimp account was used. See
> [verification status](../../../docs/verification-status.md) for what is and
> is not verified.

The Mailchimp Connector reads and writes Mailchimp audience contacts and sends
existing campaigns from Dex Flows through the Mailchimp Marketing API 3.0. It
exposes these operation-specific Dex Step factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `mailchimp.NewGetMemberStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `mailchimp.NewListMembersStep` | Query | async | `listed` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `mailchimp.NewUpsertMemberStep` | Mutation | async | `upserted` | `providerRejected`, `invalidResponse`, `defect` |
| `mailchimp.NewUpdateMemberTagsStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `mailchimp.NewSendCampaignStep` | Mutation | sync | `sent` | `alreadySent`, `notSendable`, `notFound`, `providerRejected`, `invalidResponse`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. The four audience
operations use a 30-second Execute timeout and a five-minute retry window.
`sendCampaign` uses sync durability, a 60-second Execute timeout, and a
five-minute window; see [Duplicate safety](#duplicate-safety).

This release has API-key authorization only, no Triggers, and no Studio
pickers; see [Not in this release](#not-in-this-release).

## Directory

Mailchimp is an Intuit company, so the module lives at
`connectors/intuit/mailchimp` with `metadata.company: Intuit`, the same way
Trello lives under `connectors/atlassian` and Freshdesk under
`connectors/freshworks`. The connector ID, provider, and Go package stay
`mailchimp`.

## Mailchimp setup

A connection needs one secret credential field, **api_key**: a Mailchimp
Marketing API key. Sign in, select the profile icon, choose **Profile**, open
**Extras > API keys**, choose **Create A Key**, name it, choose
**Generate Key**, and **Copy Key to Clipboard**, as Mailchimp's
[About API Keys](https://mailchimp.com/help/about-api-keys/) article describes.
Mailchimp shows the full key only once. Keys created on or after June 22, 2026
expire one year after creation; revoke a key on the same page by typing
`REVOKE`.

A key ends with a hyphen and the account's data center, such as
`-us6` at the end. Mailchimp's
[Fundamentals](https://mailchimp.com/developer/marketing/docs/fundamentals/)
page documents that suffix as the data center and the API root as
`https://<dc>.api.mailchimp.com/3.0/`, so the connector derives the host from
the key before every request and the connection has no server field. A key
without a data-center suffix, or whose suffix is not letters then digits,
selects `defect` without a request. The key is sent as
`Authorization: Bearer <key>`, which Mailchimp documents beside HTTP Basic, and
only to the derived host; redirects are never followed.

The key carries the role of the Mailchimp user who created it. Mailchimp
answers 403 to an endpoint the role does not allow and recommends an admin
user. Credentials are reread before every provider call, so replacing a key,
even with one for another data center, takes effect without a restart. The
response limit is startup configuration.

## Local configuration

Dex Web writes this record for the connection name the application uses:

```json
{
  "connectorId": "mailchimp",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp",
  "moduleVersion": "v0.1.0",
  "provider": "mailchimp",
  "connectionName": "mailchimp-audience",
  "configuration": {},
  "credentials": {"api_key": "..."}
}
```

Load it with `localconfig.LoadFromEnvironment` and
`mailchimp.NewLocalConnection`, as
[`examples/approved-campaign-send/main.go`](examples/approved-campaign-send/main.go) does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := mailchimp.NewLocalConnection(store, approvedcampaignsend.ConnectionName, connectionOptions()...)
```

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
exactly `api_key` with its data-center suffix and rejects anything else
without repeating the value.

## Contacts and the subscriber hash

Mailchimp addresses a contact in an audience by the MD5 hash of the
lowercased email address, which its
[Methods and Parameters](https://mailchimp.com/developer/marketing/docs/methods-parameters/)
page documents as the canonical identifier. `mailchimp.SubscriberHash`
computes it, and every member operation takes the address and sends the hash,
so `Jane@Example.com` and `jane@example.com` are one contact. MD5 is
Mailchimp's addressing scheme, not a security control. Results carry the hash
as `subscriberHash`.

The audience ID is Mailchimp's `list_id`. Find it under **Audience**, choose
the audience, then **More options > Audience settings**, and copy
**Audience ID**, as Mailchimp's
[Find Your Audience ID](https://mailchimp.com/help/find-audience-id/) article
describes.

Contact statuses are Mailchimp's: `subscribed`, `unsubscribed`, `cleaned`
(bounced), `pending` (sent a confirmation email), `transactional`, and, on
reads only, `archived`.

## Operations

### getMember

`getMember` reads `GET /lists/{list_id}/members/{subscriber_hash}`. A contact
in any status, including `unsubscribed`, `cleaned`, or `archived`, selects
`found`, so a Flow can see that someone opted out; Mailchimp's 404 selects
`notFound`. The Result holds the status, merge fields, and at most 50 tag
names with the total `tagCount`, as Mailchimp returns them.

### listMembers

`listMembers` reads one page of `GET /lists/{list_id}/members`:

- `pageSize` is 1 to 1000, Mailchimp's documented maximum `count`; zero reads
  100. `offset` skips matching contacts, and `nextOffset` is the following
  page's offset, or zero after the last page;
- `status` keeps one status; `changedSince` sends `since_last_changed` and
  `optedInSince` sends `since_timestamp_opt`, both in the
  `2015-10-21T15:41:36+00:00` form Mailchimp documents;
- pages are always sorted by `last_changed`, oldest first, and request only the
  fields `Member` holds with Mailchimp's `fields` parameter, so a 1000-contact
  page stays small;
- `totalItems` is Mailchimp's count of every matching contact.

Offset paging is a snapshot per page: a contact that changes while a Flow
pages moves to the end of the order, so a page can repeat it or the next page
can shift past one contact. A later read with `changedSince` catches it.

### upsertMember

`upsertMember` sends `PUT /lists/{list_id}/members/{subscriber_hash}`, which
Mailchimp documents as add or update. It is keyed by the address, so it is
idempotent by design. Mailchimp's two status fields behave differently:

- `statusIfNew` (`status_if_new`) is required and applies only when Mailchimp
  creates the contact: `subscribed` for someone who gave permission, `pending`
  to send Mailchimp's confirmation email first, `unsubscribed`, or
  `transactional`. An existing contact keeps its status, so an unsubscribed
  contact stays unsubscribed.
- `status` is optional and also changes an existing contact. `unsubscribed`
  records an opt-out. `subscribed` or `pending` would resubscribe, or email a
  confirmation to, a contact who unsubscribed or was cleaned, so either one
  requires `isResubscribeAllowed: true`, set only when the person asked to
  subscribe again; otherwise the Step selects `defect` without a request.
  `cleaned` and `archived` are never written.

`mergeFields` maps merge tags such as `FNAME` to a string, a number, or an
address object (`addr1`, `city`, `state`, `zip`), as Mailchimp's
[Merge Fields](https://mailchimp.com/developer/marketing/docs/merge-fields/)
page documents; omitted tags keep their values. `marketingPermissions`
records GDPR consent by `marketing_permission_id`.
`shouldSkipMergeValidation` sends `skip_merge_validation=true`. A rejected
contact, such as one Mailchimp reports as `Member In Compliance State` or a
permanently deleted `Forgotten Email Not Subscribed` address, selects
`providerRejected`.

### updateMemberTags

`updateMemberTags` sends `POST /lists/{list_id}/members/{subscriber_hash}/tags`
with each tag in `addTags` declared `active` and each in `removeTags`
declared `inactive`. Mailchimp creates an active tag that does not exist yet
and answers 204. `shouldSuppressAutomations` sends `is_syncing: true`, so
tag-triggered automations do not start. At most 50 tags of at most 100 bytes
each, and a tag cannot be both added and removed. A missing contact selects
`notFound`.

### sendCampaign

`sendCampaign` sends one existing campaign by ID; create and review it in
Mailchimp first. It reads `GET /campaigns/{campaign_id}` with a `fields` list,
then:

- a campaign Mailchimp shows as `sending` or `sent` selects `alreadySent`
  without a send;
- a campaign that is not a draft (`save`), such as a scheduled, paused,
  canceled, or archived one, an RSS campaign, which Mailchimp sends on its
  schedule, or one whose audience is not `expectedListId`, selects
  `notSendable` without a send;
- a draft is sent with `POST /campaigns/{campaign_id}/actions/send`, and a 204
  selects `sent`.

`expectedListId` binds the send to the audience a person approved; blank skips
the check. The Result holds the campaign as read before the send, so after
`sent` its status is still `save`.

## Duplicate safety

Mailchimp documents no idempotency key. Dex re-dispatches an async Step whose
local attempt passes about seven seconds, and it retries a Step after a lost
Worker, so the operations take these positions:

- **upsertMember** and **updateMemberTags** are safe to repeat. The PUT is
  keyed by the address and sets absolute values, and each tag is declared
  active or inactive, so a second dispatch writes the same values. Both keep
  async durability, and every unconfirmed outcome, including a 5xx or a lost
  response, is retried. Because `status_if_new` applies only to a contact
  Mailchimp creates, a repeated upsert without `status` does not change the
  status the first one set.
- **sendCampaign** is irreversible, so it runs with sync durability, and Dex
  never sends a second attempt while the first is in flight. After the read
  and just before the send, the operation records a Dex heartbeat checkpoint
  naming the Step's Call ID. A later attempt of the same Step execution that
  finds it, after a lost Worker or an Execute timeout, never sends: it reads
  the campaign again and selects `alreadySent` when Mailchimp shows it
  `sending` or `sent`, and `uncertain` otherwise.
- After the send, only an outcome that shows Mailchimp did not process it is
  retried: a 429, a 403 without an error document, which Mailchimp documents
  as throttling at exceptionally high volume, and a connection that failed
  before it opened. The checkpoint is cleared first. A 5xx, a 408, an HTML 502
  from Mailchimp's CDN, a lost or unreadable response, and an oversized or
  credential-reflecting 2xx select `uncertain`: the campaign may be sending,
  and the connector never sends it again. A new Step execution, such as the
  example's recheck Action, reads the campaign again first.

Keep `sendCampaign` sync. An application override to async durability lets
Dex dispatch a second send after seven seconds. A checkpoint lost before Dex
stored it, or a crash between the checkpoint and the send, can make an attempt
report `uncertain` for a send Mailchimp never received; that direction is
safe.

## Errors

A non-2xx response never exposes Mailchimp's `detail` or `errors[].message`,
which can repeat contact data. A Failure repeats only the problem document's
`title` when it is letters and spaces, and up to five `errors[].field` names,
such as
`Mailchimp rejected the request (HTTP 400) [Invalid Resource; fields: email_address, merge_fields.FNAME]`.
A token that contains the API key is dropped.

| Response | Reads, upsertMember, updateMemberTags | sendCampaign after its read |
| --- | --- | --- |
| 400 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 with an error document | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | `notFound` |
| 405, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429, 403 without an error document | Retry after `Retry-After` when present | Retry, checkpoint cleared |
| connection refused before sending | Retry | Retry, checkpoint cleared |
| 408, 5xx, lost or unreadable response | Retry | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input or connection credentials | `defect`, with no request | `defect`, with no request |

The campaign read before a send maps like a read, because nothing has been
sent yet. The Receipt carries the Call ID, the subscriber hash, audience ID, or
campaign ID, and Mailchimp's `X-Request-Id` header when present.

## Not in this release

### OAuth 2

Mailchimp's
[OAuth 2 guide](https://mailchimp.com/developer/marketing/guides/access-user-data-oauth-2/)
authorizes at `https://login.mailchimp.com/oauth2/authorize`, exchanges the
code with a form-encoded POST to `https://login.mailchimp.com/oauth2/token`,
and issues tokens that do not expire. Those endpoints are static, but the
connection would not work through Dex Web:

- Mailchimp OAuth has no scopes, and the manifest schema requires at least one
  scope, which Dex Web then requires in the token response's `scope` string.
- The data center is known only after the exchange, from
  `GET https://login.mailchimp.com/oauth2/metadata` with an
  `Authorization: OAuth <token>` header. Dex Web's credential derivation sends
  `Bearer`, and Mailchimp does not document that the metadata endpoint
  accepts it.
- Mailchimp documents neither `token_type`, `expires_in`, nor `scope` in its
  token response.

Mailchimp's own guidance is to use an API key for code tied to your own
account, and OAuth for integrations that access other users' accounts.

### Triggers

Mailchimp's
[webhook guide](https://mailchimp.com/developer/marketing/guides/sync-audience-data-webhooks/)
delivers audience events as form-encoded POSTs, retries for 75 minutes, and
offers optional HMAC-SHA256 signing in `X-Mailchimp-Signature` with a secret
shown once. Deliveries carry no event ID, only `type`, `fired_at`, and the
contact, so a Trigger has no provider-stable event ID to deduplicate on.
Mailchimp is also widely reported to validate a callback URL with a `GET`
before it saves a webhook; the developer guide fetched for this release does
not state it, and `webhooktrigger.Endpoint` answers only `POST`. The connector
declares no Trigger until both are resolved. Until then, poll with
`listMembers` and `changedSince`.

### Studio pickers

An audience picker would list `GET /lists`, but Studio setup commands declare
one fixed HTTPS host, and every Mailchimp account has its own
`https://<dc>.api.mailchimp.com` host, which the iframe cannot learn because it
never sees the key. Mailchimp publishes no list of data centers to declare one
command per host. The audience and campaign IDs are therefore operation input,
with the paths above.

## Example

[`examples/approved-campaign-send`](examples/approved-campaign-send) is a
runnable Dex Web **Start Flow** example that uses all five operations: it reads
each contact, leaves unsubscribed, cleaned, and archived contacts untouched,
upserts and tags the rest, counts the subscribed audience, and waits for a
person to approve the campaign send through a permissioned Action before it
sends the campaign at most once.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the example owns its real
Worker, retry, persistence, duplicate-dispatch, Action, and transition
coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/intuit/mailchimp/connector.yaml
go run ./cmd/connectorctl generate --check connectors/intuit/mailchimp/connector.yaml
```

The provider fakes cover the derived data-center host and Bearer header,
invalid key suffixes, the subscriber hash path, `status_if_new` without
`status`, the resubscribe guard, merge field shapes, tag declarations and
`is_syncing`, list bounds, filters, order, and `nextOffset`, the campaign read
and every status, the dispatch checkpoint and its clearing, replayed attempts,
Mailchimp problem titles and field names without detail text, `Retry-After`,
throttling 403s, redirects, and oversized, malformed, and credential-reflecting
responses.

No live Mailchimp account was used. The following live behavior is
unverified: whether Mailchimp accepts `Bearer` on every endpoint used; the exact
problem titles for a compliance-state or permanently deleted contact; whether
a repeated `PUT` with `status_if_new: pending` sends no second confirmation
email; whether re-declaring an active tag restarts a tag-triggered automation;
whether `fields` returns exactly the documented member and campaign fields;
whether `POST /actions/send` moves a campaign to `sending` before it answers
and rejects a second send of a campaign that is sending; whether a 429 or
throttling 403 is always returned before a send is processed; and the
Profile > Extras > API keys path and key expiry in the setup guidance.
