# Pipedrive Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Pipedrive; no live Pipedrive company was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module searches, lists, reads, creates, upserts, and updates Pipedrive
persons, organizations, and deals through Pipedrive API v2. It is the CRM
system-of-record path for a Flow: identify a person by email, find the
organization and the open deal that belong to them, and move that deal to a
stage or create it.

| Operation | Kind | Durability | Pipedrive endpoint | Happy branch | Other branches |
| --- | --- | --- | --- | --- | --- |
| `searchObjects` | Query | async | `GET /api/v2/{objectType}/search` | `searched` | `providerRejected`, `invalidResponse`, `defect` |
| `getObject` | Query | async | `GET /api/v2/{objectType}/{id}` | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `listObjects` | Query | async | `GET /api/v2/{objectType}` | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `upsertObject` | Mutation | sync | search, then `POST /api/v2/{objectType}` or `PATCH .../{id}` | `upserted` | `multipleMatches`, `providerRejected`, `invalidResponse`, `uncertain`, `defect` |
| `createObject` | Mutation | sync | `POST /api/v2/{objectType}` | `created` | `providerRejected`, `uncertain`, `defect` |
| `updateObject` | Mutation | async | `PATCH /api/v2/{objectType}/{id}` | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |

`ObjectType` is `persons`, `organizations`, or `deals`. Only each operation's
happy branch is required; an unwired optional branch fails the Flow when
selected. Every operation uses a 30-second Execute timeout and five attempts
within two minutes, and keeps all of an attempt's requests, including a token
refresh, inside a 25-second deadline; one request times out after 20 seconds
unless `WithHTTPClient` supplies a client with its own `Timeout`. This release
has no Triggers; see [Triggers](#triggers).

## The record-store shape

The operation names, branch names, and record shape follow the HubSpot
connector, so a CRM process swaps vendors by changing only the vendor values:

| Concept | HubSpot | Pipedrive |
| --- | --- | --- |
| Object types | `contacts`, `companies`, `deals` | `persons`, `organizations`, `deals` |
| Find a person by email | `searchObjects` on `email` with `EQ` | `searchObjects` with `Fields: [email]`, `ExactMatch: true` |
| Idempotent person write | `upsertObject`, `IDProperty: email` | `upsertObject`, `IDProperty: email` |
| Organization identity | a unique custom property | `upsertObject`, `IDProperty: name` |
| Create a deal | `upsertObject` on a unique property | `createObject` (sync, dispatch checkpoint) |
| Stage and owner | `dealstage`, `hubspot_owner_id` properties | `stage_id`, `owner_id` fields |
| Changed since | `searchObjects` on `hs_lastmodifieddate` | `listObjects` with `UpdatedSince` |
| Paging | `NextAfter` | `NextCursor` |

`CRMObject` carries `ID`, `Name` (a deal's `title`), and the association graph
as decimal ID strings: `OwnerID`, `OrganizationID` (`org_id`), `PersonID`,
`PipelineID`, and `StageID`, plus a deal's `Status`, a person's `Emails`
(primary first), `AddTime`, and `UpdateTime`. `Fields` keeps every returned
standard field's exact JSON value and `CustomFields` keeps every custom field
by its 40-character key; read them with `DecodeField` and `DecodeCustomField`.
Writes take the same two maps of exact JSON values with Pipedrive API v2 field
names; `pipedrive.StringValue` and `pipedrive.IDValue` encode text and IDs. A
value that is not one JSON value, a custom field named by its label, or more
than 100 values select `defect` before any request.

Pipedrive's own values pass through unmapped: deal `status` is `open`, `won`,
`lost`, or, for `listObjects` only, `deleted`; stage, pipeline, and owner are
Pipedrive's numeric IDs; custom field values keep Pipedrive's types, such as an
option ID for a single-option field. A process that compares vendors maps these
values itself, where a reader can see it.

## Authentication

A connection uses one of two methods.

**Personal API token** (`api-token`, the default and recommended method) serves
one Pipedrive company with the visibility and permissions of the user whose
token it is. The user copies the 40-character token from
`https://app.pipedrive.com/settings/api` (account name > Company settings >
Personal preferences > API); a company admin can turn on **use API** for a
permission set under Settings > Manage users > Permission sets. The token is
sent only in the `x-api-token` header to `https://api.pipedrive.com`, the host
in Pipedrive's API v2 reference; the `endpoint` setting can name the company
domain, such as `https://acme.pipedrive.com`, which Pipedrive also suggests.
Only HTTPS hosts under `pipedrive.com` are accepted, or a loopback URL for
tests. The token does not expire; after the user generates a new one, paste the
replacement.

**Pipedrive OAuth app** (`pipedrive-oauth`) serves a private or public app
registered in Pipedrive Developer Hub, which needs a free developer sandbox
account. Register the Redirect URI Dex Web shows as the app's Callback URL,
turn on `deals:full` for deals and `contacts:full` for persons and
organizations (`base` is always on), and paste the client ID and secret.
Pipedrive takes the scopes from the app, not from the authorization request.
The token response carries `api_domain`,
the company's own API host, which Dex Web maps into the `api_domain`
credential; every request of an OAuth connection goes there with a bearer
token, and the `endpoint` setting is ignored. Access tokens last one hour.
`CredentialRefreshDriver` refreshes them through `sdkgo/oauthtoken` at
`https://oauth.pipedrive.com/oauth/token` with HTTP Basic client
authentication, as Pipedrive recommends, keeps Pipedrive's unchanged refresh
token, and stores the `api_domain` of every refresh. A connection without a
valid `api_domain` refreshes before its first call. `invalid_grant`,
`invalid_client`, `unauthorized_client`, or a refreshed grant without
`deals:full` and `contacts:full` marks the connection
`reauthorization_required`; a 5xx, a 429, a transport failure, or a response
without a valid `api_domain` is retried. After Pipedrive rejects an OAuth access
token with `401`, the connector asks once for a refresh, which project storage
performs only when the stored expiry has passed, and resends once, because
Pipedrive rejects an unauthenticated request before acting on it.

Both methods ship because they serve different owners: a token is the
shortest setup for a company's own automation and the only method whose Studio
pickers work, while OAuth suits an app installed into several companies and
keeps tokens short-lived.

## Operations

### searchObjects

`SearchObjectsInput` sends `term` to Pipedrive's search endpoint for one object
type, optionally limited to `Fields` (persons: `name`, `email`, `phone`,
`notes`, `custom_fields`; organizations: `name`, `address`, `notes`,
`custom_fields`; deals: `title`, `notes`, `custom_fields`) and to
`ExactMatch`, which Pipedrive applies without case. Persons and deals filter by
`OrganizationID`, deals by `PersonID` and `Statuses`. A term needs 2
characters, or 1 with `ExactMatch`. `Limit` is 1 through 500 and defaults to
25; pass `NextCursor` back as `Cursor`. Results are partial records
(`IsPartial`), Pipedrive's search summary with IDs and links; hydrate one with
`getObject`. Newly written records can take a moment to appear in search, and
the Search API allows only 10 requests per 2 seconds.

### getObject and listObjects

`getObject` reads one record with every field; a missing or invisible record
selects `notFound`. `listObjects` reads one page of full records filtered by
`UpdatedSince` and `UpdatedUntil` (`update_time`, RFC 3339 in UTC), owner,
organization, and, for deals, person, pipeline, stage, and statuses. With
`UpdatedSince` and no `SortBy` it sorts by `update_time`, so a process can keep
the last `UpdateTime` it saw as its changed-since checkpoint. `CustomFieldKeys`
limits returned custom fields to 15 keys.

### upsertObject: persons by email, organizations by name

`UpsertObjectInput` names `email` for persons or `name` for organizations as
`IDProperty`, and the value in `IDValue`. The connector runs Pipedrive's exact
search on that field and keeps only results whose email or name really equals
the value, ignoring case, so a look-alike address is never a match. One match
is updated with `PATCH` and the result reports `Created: false`; no match is
created, with the email written as the person's primary work email or the name
as the organization's name, and the result reports `Created: true`. More than
one match selects `multipleMatches` with `MatchingIDs` and writes nothing,
because Pipedrive does not enforce either value as unique. A person upsert
requires `Fields["name"]`, and neither type may set the identity field itself.

### createObject

`createObject` creates one person, organization, or deal from `Fields` and
`CustomFields`. A deal requires `title`; a person or organization requires
`name`.

### updateObject

`updateObject` sets the named fields on one record with `PATCH`, such as a
deal's `stage_id` and `owner_id`; fields it does not name keep their values.

## Duplicate safety

Pipedrive documents no idempotency key, and Dex dispatches an async Step again
when its local attempt passes about seven seconds or a Worker is lost, so the
two creating operations use **sync** durability and a Dex heartbeat
checkpoint recorded before the create is sent:

- `createObject` does not send its create again in a later attempt of the
  same Step execution, within the limit described below. A burst-limit
  `429` or a connection that never opened clears the checkpoint and retries. A
  lost response, a 5xx, a timeout, or an unreadable success selects
  `uncertain`, and a later attempt that finds the checkpoint selects
  `uncertain` without sending anything, so a person decides whether the record
  exists.
- `upsertObject` searches before every create. An unconfirmed create returns
  Retry with the checkpoint kept; the next attempt searches again and updates
  the record the lost attempt created. It selects `uncertain` instead when the
  search still finds no record, for example while Pipedrive's search index
  lags, or when that attempt's search or update fails in a way it does not
  retry, such as a `403` or an invalid response, so `providerRejected` always
  means nothing was created.
- `updateObject` writes absolute values, so a repeat converges and it keeps
  async durability; it retries every unconfirmed outcome and never selects
  `uncertain`. A repeat overwrites a change another user made in between.

The checkpoint narrows the double-send window but does not close it. Dex
accepts the checkpoint when the Worker writes it to its stream, before the
request leaves; a Worker lost in that instant, before Dex stored the
checkpoint, could still send twice, and a crash after the checkpoint but
before the request left reports `uncertain` for a request Pipedrive never
received. For `upsertObject` the identity search before every create narrows
the first case further: the repeat finds the earlier person or organization
unless Pipedrive's search index has not caught up.

The example's real-Dex tests prove these with a fake Pipedrive that answers
after nine seconds, drops the connection after applying a create, and holds a
create while the Worker is replaced.

## Branches, errors, and rate limits

| Pipedrive response | Result |
| --- | --- |
| `400`, `422` | `providerRejected`, `VALIDATION` |
| `401` | `providerRejected`, `AUTHENTICATION`, after one OAuth refresh and resend only when the stored token has expired |
| `402` | `providerRejected`, `PROVIDER_REJECTION` (company account needs payment) |
| `403`, including Pipedrive's block of API traffic that ignores `429` | `providerRejected`, `AUTHORIZATION` |
| `404` | `notFound` on `getObject` and `updateObject`; `providerRejected` otherwise |
| `429` with `x-daily-ratelimit-token-remaining: 0` | `providerRejected`, `QUOTA_EXHAUSTED`; the company's daily token budget resets at midnight in Pipedrive's server time zone |
| other `429` (burst limit) | Retry after the longer of `Retry-After` and `x-ratelimit-reset`; the reset counts as at least Pipedrive's 2-second window and at most 10 seconds |
| `408`, `5xx` except `501` | Retry, honoring `Retry-After`; `uncertain` for `createObject` |
| connection never opened | Retry, nothing was sent |
| dropped connection after sending | Retry; `uncertain` for `createObject` |
| oversized, malformed, `success: false`, or ID-less success | `invalidResponse`; `uncertain` for `createObject` |
| a response that echoes the credential | `invalidResponse`, and the body is discarded |
| invalid input or unusable credentials | `defect`, no request |

Failure messages name only the HTTP status, never Pipedrive's `error` or
`error_info` text. Receipts carry Pipedrive's `X-Correlation-Id`. Every request
also spends the company's daily API token budget (30,000 tokens times the plan
multiplier times seats, shared by every integration): a get costs 1 token, a
create or update 5, a list 10, and a search 20 in API v2.

## Studio units

| Unit | Outputs | Source (Personal API token connections) |
| --- | --- | --- |
| `ownerPicker` | `ownerId` | `GET https://api.pipedrive.com/v1/users` |
| `dealStagePicker` | `pipelineId`, `stageId` | `GET /api/v2/pipelines` and `/api/v2/stages`, paged with `cursor` |
| `customFieldPicker` | `objectType`, `fieldKey` | `GET /api/v2/dealFields`, `/personFields`, or `/organizationFields`, paged with `cursor` |

The Dex Web broker sends each read-only command to `https://api.pipedrive.com`
with the connection's `api_token` in the `x-api-token` header; the bundle never
receives it. Studio commands reach only fixed hosts, so an OAuth connection,
whose API host is its own company domain, cannot list anything; its units, and
every unit whose list cannot load, accept typed IDs instead. Listing for OAuth
connections needs a Studio command whose host comes from the connection's
`api_domain`.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/deal-intake/main.go`](examples/deal-intake/main.go) does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
settings, err := loadSettings(project.Configuration)
if err != nil {
	return err
}
connection, err := pipedrive.NewProjectConnection(project, dealintake.ConnectionName)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Credentials stay
in project storage and are read for every call; an OAuth connection's token is
refreshed when it is missing, has no recorded expiry, expires within five
minutes, or has no valid `api_domain`.

A Flow builds write inputs from its own state with the value helpers, as the
example's person upsert does:

```go
// MapToUpsertLeadPersonInput upserts the person by email with the name, the one matching organization, and the owner.
func (flow *Flow) MapToUpsertLeadPersonInput(intake DealIntake) pipedrive.UpsertObjectInput {
	fields := map[string]json.RawMessage{"name": pipedrive.StringValue(intake.Lead.Name)}
	if intake.OrganizationID != "" {
		fields["org_id"] = pipedrive.IDValue(intake.OrganizationID)
	}
	if flow.settings.LeadOwner.OwnerID != "" {
		fields["owner_id"] = pipedrive.IDValue(flow.settings.LeadOwner.OwnerID)
	}
	return pipedrive.UpsertObjectInput{
		ObjectType: pipedrive.ObjectTypePersons, IDProperty: pipedrive.IdentityPropertyEmail, IDValue: intake.Lead.Email, Fields: fields,
	}
}
```

## Example

[`examples/deal-intake`](examples/deal-intake) finds the lead's organization
by exact name, upserts the lead person by email, lists the person's open deal
in the configured pipeline, moves it to the configured stage and owner or
creates the deal there with the lead source in a picked custom field, and reads
the deal back. It composes all three Studio units.

## Triggers

This release has no Trigger. Pipedrive webhooks v2 authenticate delivery only
with HTTP Basic credentials set on the subscription, without a signature, and
need no handshake, so `sdkgo/webhooktrigger` could verify them with a
constant-time comparison in `VerifyRequest`, as strong as the static token
scheme of the `superdurable/webhook` connector. A Trigger would need:
webhook username and password credential fields; `meta.id` as the event ID and
`meta.timestamp` as the occurrence time; one endpoint runner per connection;
binding configuration for entity and action with a Studio unit, because Dex Web
cannot save binding configuration without one; and documentation that a
captured delivery can be replayed while the credentials stay unchanged, so the
application deduplicates on `meta.id`. Pipedrive answers each delivery within
10 seconds and retries three times, after 3, 30, and 150 seconds. A
changed-since poll Trigger instead needs a durable per-binding cursor, which
`sdkgo` does not provide; until then a Flow can poll with `listObjects`.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/deal-intake/flow/... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration tests need a running `dexcli dev` and use a local fake
Pipedrive; no live Pipedrive company is required.

Not verified against live Pipedrive: every response shape beyond the `401`
body that placeholder credentials returned (`{"success":false,"error":...,
"errorCode":401}` with `x-correlation-id`); the daily-budget `429` and its
`x-daily-ratelimit-token-remaining` value; search-index lag after a create;
Dex Web's OAuth callback against Pipedrive, including whether Pipedrive's
comma-separated `scope` string satisfies Dex Web's scope check and whether its
form-encoded exchange works (a placeholder probe showed Pipedrive reads client
credentials from the form body); refresh and `api_domain` rotation; and the
three pickers against a real company.
