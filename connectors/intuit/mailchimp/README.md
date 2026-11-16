# Mailchimp Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp/go@v0.24.0`. It requires Connector Go SDK `sdkgo` v0.38.0, the release of the durable poll Trigger kit.

The TypeScript package `@superdurable/connector-intuit-mailchimp@0.2.0` implements all five operations and the `memberChanged` Trigger with `@superdurable/connectors-sdk@0.12.0`. Its Trigger reads and writes the Go poll cursor document and keyed snapshot byte for byte and yields the same Trigger event IDs, so Go and TypeScript replicas can share one binding. See its [package README](typescript/README.md), the [runnable get-member Flow](typescript/examples/get-member/README.md), and the [runnable member-changed Flow](typescript/examples/member-changed/README.md). Its provider replay and Dex integration tests use local stand-ins; live Mailchimp behavior remains unverified.

Moved from `github.com/superdurable/dex-connectors-library/connectors/intuit/mailchimp`.

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
operations use a 30-second Execute timeout and the one-hour default retry budget.
`sendCampaign` uses sync durability, a 60-second Execute timeout, and the
one-hour default retry budget; see [Duplicate safety](#duplicate-safety).

The `memberChanged` Trigger delivers each contact added to or changed in one
audience by polling; see [memberChanged Trigger](#memberchanged-trigger).

This release has API-key authorization only, a webhook Trigger is deferred,
and the audience ID is typed rather than picked; see
[Not in this release](#not-in-this-release).

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

## Project configuration

Dex Web or Superverse Studio writes the connection to the project
configuration. Load it with `projectconfig.LoadFromEnvironment` and open the
connection by the name the application uses with
`mailchimp.NewProjectConnection`, as
[`examples/approved-campaign-send/main.go`](go/examples/approved-campaign-send/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := mailchimp.NewProjectConnection(project, approvedcampaignsend.ConnectionName, connectionOptions()...)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration loading](../../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `api_key`.

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

Mailchimp documents no idempotency key. Dex can replay a Step after Worker
loss or a result lost before commit, so the operations take these positions:

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
  and the connector never sends it again. A new Step execution reads the
  campaign again first.

Keep `sendCampaign` sync so its result is committed promptly. A checkpoint lost before Dex
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

## memberChanged Trigger

`memberChanged` starts one event per contact version added to or changed in
one audience: a signup, an API or import add, a subscribe or unsubscribe, a
profile, merge field, tag, or email change, or a cleaned address.
It polls
[`GET /lists/{list_id}/members`](https://mailchimp.com/developer/marketing/api/list-members/list-members-info/)
with `since_last_changed`, `sort_field=last_changed`, `sort_dir=ASC`, and
`count=250`, using the same API key and the same audience access as
`listMembers`, so it needs no new permission.

| Behaviour | Detail |
| --- | --- |
| Baseline | The first poll reads the most recently changed page (`sort_dir=DESC`, `count=250`), keeps the newest `last_changed` second, and snapshots the contacts changed in that second. Contacts changed before it are never delivered; a contact changed later in that second is. When all 250 contacts share that second, the baseline moves one second later and none of that second is delivered. An empty audience has no baseline, so its first contact is delivered. |
| Passes | Each poll starts five minutes before the newest change time read so far, never earlier than one second before the baseline, so a change that becomes listable after a later one is still delivered. A keyed snapshot of the 10,000 most recently changed contacts, committed with the cursor, drops versions already delivered. |
| Pagination | A full page restarts the next request one second before its last change time, because `last_changed` has one-second resolution. A page whose contacts all share one second continues by `offset`, up to one million contacts in that second. Up to 10 pages are read per poll; a longer backlog continues on the next poll. |
| Event ID | `mailchimp-` followed by a SHA-256 prefix of the audience ID and the listed contact version (subscriber hash, `last_changed`, status, and every other listed field), so a refetch after a crash yields the same ID and starts no second Flow, and two changes in one second that leave different content are both delivered. |
| Change kind | Mailchimp reports none, so a new contact and an edited one look alike. A contact changed several times between polls is delivered once, in its latest state. Deleted contacts are not listed and are not delivered. |
| Status filter | The binding's statuses keep only contacts in one of those statuses after the change; selecting none delivers every status. A filtered-out change still moves the cursor. |
| Rate limits | Mailchimp allows [10 simultaneous requests per user](https://mailchimp.com/developer/marketing/docs/errors/) and answers 429 beyond that. The runner sends one request at a time for all bindings of a connection. A 429, or a 403 without an error document (Mailchimp's throttling), pauses every binding of the connection for `Retry-After` when present, at least one poll interval, and otherwise backs off exponentially. |
| Cursor reset | A 400 for the poll query, an undecodable cursor, or a cursor saved for another audience records a new baseline, increments the cursor's reset count, and logs `poll cursor invalid; reseeding` at WARN. Changes in the gap are not delivered. A 401, a 403 with an error document, a 404 for the audience, or a 5xx backs off per binding up to 15 minutes. |

The connection's `pollInterval` setting (at least 5 seconds) sets the delay
between polls. Each binding saves two values:

- `audienceId`, entered with the `audienceIdEntry` unit. In Mailchimp choose
  **Audience**, pick the audience in the **Audience** drop-down if there are
  several, then **More options > Audience settings** and copy **Audience ID**,
  as [Find your audience ID](https://mailchimp.com/help/find-audience-id/)
  describes. It is 1 to 64 letters and digits, such as `57afe96172`, and is
  required. Changing it records a new baseline.
- `statuses`, selected with the `memberStatusPicker` unit.

`mailchimp.NewProjectMemberTriggerRunner` loads the saved bindings and records
each change in the binding's project Trigger inbox before its cursor commits
in the binding's project poll cursor, so a restarted or replaced replica
neither misses nor repeats a change, as
[`examples/member-change-intake/main.go`](go/examples/member-change-intake/main.go) does:

```go
triggerRunner, err := mailchimp.NewProjectMemberTriggerRunner(project, memberchangeintake.ConnectionName, mailchimp.ProjectMemberTriggerRunnerConfig{
	MemberChangedRoutes: []mailchimp.ProjectMemberChangedTriggerRoute{{
		BindingName: memberchangeintake.MemberChangedTriggerBinding,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, memberchangeintake.AcceptEveryMemberChange, memberchangeintake.ResolveFlowID,
			memberchangeintake.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, mailchimp.WithLogger(logger))
```

`mailchimp.NewMemberTriggerRunner` takes targets and cursor stores directly.
The generated `mailchimp.NewMemberChangedTrigger` keeps its cursor in memory,
so it records a new baseline on every start. `mailchimp.WithLogger` routes the
runner's records; they carry binding identity and never contact data.

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

### Webhook Trigger

Mailchimp's
[webhook guide](https://mailchimp.com/developer/marketing/guides/sync-audience-data-webhooks/)
delivers audience events as form-encoded POSTs, retries for 75 minutes, and
offers optional HMAC-SHA256 signing in `X-Mailchimp-Signature` with a secret
shown once. Deliveries carry no event ID, only `type`, `fired_at`, and the
contact, so a Trigger has no provider-stable event ID to deduplicate on.
Mailchimp is also widely reported to validate a callback URL with a `GET`
before it saves a webhook; the developer guide fetched for this release does
not state it, and `webhooktrigger.Endpoint` answers only `POST`. The connector
declares no webhook Trigger until both are resolved; the
[memberChanged Trigger](#memberchanged-trigger) polls instead.

### Studio pickers

An audience picker would list `GET /lists`, but Studio setup commands declare
one fixed HTTPS host, and every Mailchimp account has its own
`https://<dc>.api.mailchimp.com` host, which the iframe cannot learn because it
never sees the key. Mailchimp publishes no list of data centers to declare one
command per host. The audience and campaign IDs are therefore operation input,
with the paths above, and the `memberChanged` binding's audience is typed into
the `audienceIdEntry` unit, which states the Mailchimp menu path.

## Mocks

`mailchimpmock.New` supplies one coherent audience: Ben, Ed, and Fay are
subscribed to audience `57afe96172`, Ben has the spring launch tag, and draft
campaign `42694e9e57` targets that audience. `listMembers` serves three
oldest-first pages. Named cases cover missing contacts and audiences, denied
access, rejected writes, and campaigns that are already sent, unsendable, or
missing. Script a different case when a test uses another audience or campaign.

The [runnable integration test](go/examples/approved-campaign-send/flow/workflow_mock_integration_test.go)
starts the example Flow on a real Dex Worker, approves the send, and checks its
mapped operation inputs:

```go
mock := mailchimpmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

```go
sends := mock.SendCampaign().Calls()
require.Len(t, sends, 1)
require.Equal(t, mailchimp.SendCampaignInput{CampaignID: integrationCampaignID, ExpectedListID: integrationListID}, sends[0].Input)
```

See [`sdkgo/connectormock`](../../../sdkgo/README.md#connector-mocks) for scripting.

## Example

[`examples/approved-campaign-send`](go/examples/approved-campaign-send) is a
runnable Dex Web **Start Flow** example that uses all five operations: it reads
each contact, leaves unsubscribed, cleaned, and archived contacts untouched,
upserts and tags the rest, counts the subscribed audience, and waits for a
person to approve the campaign send through a permissioned Action before it
sends the campaign at most once.

[`examples/member-change-intake`](go/examples/member-change-intake) starts
one Flow per contact change through the `memberChanged` Trigger and reads the
contact back with `getMember`.

## Python package

[`python/`](python/) contains the `dex-connector-intuit-mailchimp` v0.2.0 package,
on `dex-connectors-sdk>=0.12.0`. It implements all five operations with
manifest-generated models, Step factories, connection bindings, and mocks, plus
a handwritten provider client. It also implements the `memberChanged` Trigger
at behavior parity with Go: it reads and writes the same poll cursor document
and keyed snapshot byte for byte and yields the same Trigger event IDs, so Go
and Python replicas can share one binding. The
[`get_member` example](python/examples/get_member/README.md) and the
[`member_changed` example](python/examples/member_changed/README.md) are
runnable Dex Flows with the static `mailchimp-audience` connection and real
Worker and Client integration tests. The TypeScript package implements the same
`memberChanged` Trigger. See the package README for installation, testing, and
live-provider limits.

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
responses. The audience fake behind the `memberChanged` tests models
`since_last_changed`, ascending and descending `last_changed` order, `count`,
and `offset`, and covers the baseline, the lookback, ties within one second,
the status filter, 429 with `Retry-After`, throttling 403s, the 400 reset, and
corrupt or foreign cursors; the example's real Dex tests cover delivery across
a restart, the 400 reset, and a rate-limit pause.

No live Mailchimp account was used. The following live behavior is
unverified: whether Mailchimp accepts `Bearer` on every endpoint used; the exact
problem titles for a compliance-state or permanently deleted contact; whether
a repeated `PUT` with `status_if_new: pending` sends no second confirmation
email; whether re-declaring an active tag restarts a tag-triggered automation;
whether `fields` returns exactly the documented member and campaign fields;
whether `POST /actions/send` moves a campaign to `sending` before it answers
and rejects a second send of a campaign that is sending; whether a 429 or
throttling 403 is always returned before a send is processed; and the
Profile > Extras > API keys path and key expiry in the setup guidance; whether
`since_last_changed` is exclusive and compared at one-second resolution, how
soon a change becomes listable, whether archived contacts are listed without a
`status` filter, whether `Retry-After` is ever sent, and the
Audience > More options > Audience settings path.
