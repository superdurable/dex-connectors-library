# HubSpot Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/hubspot/go@v0.25.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/hubspot`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for HubSpot; no live HubSpot account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module searches, reads, upserts, and updates HubSpot CRM contacts,
companies, and deals through HubSpot's 2026-09 CRM object API. It is the
system-of-record write path for a Flow: identify a person by email, find the
deal that belongs to them, and move that deal forward.

| Operation | Kind | HubSpot endpoint | Happy branch | Languages |
| --- | --- | --- | --- | --- |
| `searchObjects` | Query | `POST /crm/objects/2026-09/{objectType}/search` | `searched` | Go, Python, TypeScript |
| `getObject` | Query | `GET /crm/objects/2026-09/{objectType}/{recordId}` | `found` | Go, Python, TypeScript |
| `upsertObject` | Mutation | `POST /crm/objects/2026-09/{objectType}/batch/upsert` with one input | `upserted` | Go, Python, TypeScript |
| `updateObject` | Mutation | `PATCH /crm/objects/2026-09/{objectType}/{recordId}` | `updated` | Go, Python, TypeScript |

It also exposes one Trigger:

- `crmObjectChanged` delivers each contact, company, or deal of one object
  type that is created or changed, read by polling CRM search on the
  last-modified date. See [crmObjectChanged Trigger](#crmobjectchanged-trigger).

`ObjectType` is `contacts`, `companies`, or `deals`. Record IDs are decimal
strings. Only the record ID, returned property values, timestamps, archived
flag, and HubSpot app link cross the connector boundary; access tokens, client
secrets, refresh tokens, and HubSpot message text never do.

## Authentication

A connection uses one of two methods.

**Private app access token** (`private-app-token`, the default and
recommended method) serves one HubSpot account. In HubSpot, a super admin
opens **Development** > **Legacy apps** > **Create legacy app** > **Private**,
adds the scopes below, creates the app, and copies the `pat-na1-...` or
`pat-eu1-...` token from **Auth** > **Show token**. A static-auth app on the
HubSpot developer platform works the same way. The token does not expire; after
rotating it in HubSpot, paste the replacement into the connection.

**HubSpot OAuth** (`hubspot-oauth`) serves an app installed through HubSpot's
consent screen. Register the Redirect URI Dex Web shows on the app's **Auth**
tab, configure the same scopes, and paste the client ID and secret. Dex
requests `oauth` plus the scopes below. Access tokens last 30 minutes;
`CredentialRefreshDriver` exchanges the refresh token at
`https://api.hubapi.com/oauth/2026-09/token` with the client credentials in the
form body. HubSpot does not document refresh-token rotation, so every refresh
result carries the next refresh token: the one HubSpot returned, or the prior
one. `invalid_grant` (HubSpot status `BAD_REFRESH_TOKEN`), `invalid_client`,
`unauthorized_client`, or a refreshed grant missing a required scope marks the
connection `reauthorization_required`; a 5xx or transport failure is retried.
After HubSpot rejects an OAuth access token with `401`, the connector asks once
for a refresh, which project storage performs only when the stored expiry has
passed, and then resends the request once; otherwise the `401` selects
`providerRejected` with `AUTHENTICATION`. The older `/oauth/v1/token` endpoint
is deprecated on 2027-02-16, and this connector never calls it.

Both methods need these scopes:

- `crm.objects.contacts.read`, `crm.objects.contacts.write`
- `crm.objects.companies.read`, `crm.objects.companies.write`
- `crm.objects.deals.read`, `crm.objects.deals.write`
- `crm.objects.owners.read`, for the owner picker

Dex Web `cli-v1.1.0` checks granted OAuth scopes only in the RFC 6749 `scope`
string of the token response. HubSpot reports them in a `scopes` array, so a
local Dex Web OAuth callback for this connector is expected to fail with
`CONNECTOR_OAUTH_SCOPE_INSUFFICIENT`. Use the private app token method with
local Dex Web until Dex Web accepts HubSpot's `scopes` array.

## Operations

### searchObjects

`SearchObjectsInput` mirrors HubSpot's search body with typed values:

- `FilterGroups` are OR-ed and each group's `Filters` are AND-ed; HubSpot
  allows 5 groups, 6 filters per group, and 18 filters in total.
- A `SearchFilter` names a property, or an association pseudo-property such as
  `associations.contact`, and one `FilterOperator`. `EQ`, `NEQ`, `LT`, `LTE`,
  `GT`, `GTE`, `CONTAINS_TOKEN`, and `NOT_CONTAINS_TOKEN` take `Value`;
  `BETWEEN` takes `Value` and `HighValue`; `IN` and `NOT_IN` take `Values`
  (lowercase for string properties); `HAS_PROPERTY` and `NOT_HAS_PROPERTY`
  take none. Dates are Unix milliseconds.
- `Query` searches the object's default text properties, `Sort` orders by one
  property, and `Properties` selects the returned values.
- `Limit` is 1 through 200 and defaults to 10. `After` is the previous page's
  `NextAfter`; paging cannot pass HubSpot's 10,000th result.

Invalid input, including a body over HubSpot's 3,000-character limit, selects
`defect` with no request. The `ObjectPage` result holds `Total`, the page's
`Objects`, and `NextAfter`, which is empty on the last page. Newly written
records can take a few moments to appear in HubSpot search.

### getObject

`GetObjectInput` names the object type, the record ID, and optional
`Properties`. A missing or archived record selects `notFound`.

### upsertObject

`UpsertObjectInput` names a unique identifier property in `IDProperty`, such
as `email` for contacts or any custom property created with unique values, and
its value in `IDValue`. HubSpot creates the record when no record has that
value and otherwise updates the record that does. This is the idempotent create
a retried Step needs: a Dex retry, a Worker restart, or an asynchronous
fallback dispatch of the same Step converges on one record instead of creating
a duplicate. `UpsertedObject.Created` reports what the answered dispatch did,
so it can be false after a retry even though an earlier dispatch created the
record.

Both Mutations declare `repeatOnUnknown: safe`. If HubSpot answers
`409 CONFLICT`, the connector resends the same upsert once in the same attempt.
A concurrent dispatch of the same upsert may have created the record first, and
the resend updates it. An HTTP `409` does not prove that HubSpot refused the
write, so a second one returns Retry; an email another contact owns therefore
retries until the Step retry policy is exhausted. Only a conflict in HubSpot's
2xx batch answer selects `conflict` for `upsertObject`. `updateObject` keeps
selecting `conflict` for HubSpot's `409 CONFLICT`.

### updateObject

`UpdateObjectInput` sets named property values on one record ID; an empty
string clears a value. Repeating an update sets the same values again. The
repeat overwrites a change another user made to the same property in between,
as any last-write-wins retry does.

## Branches, errors, and retries

| HubSpot response | Result |
| --- | --- |
| `400`/`422`, or `VALIDATION_ERROR` on a read | `providerRejected`, `VALIDATION` |
| a Mutation's `3xx`, `501`, or `4xx` other than `400`, `401`, `403`, `404`, `409`, `411`, `413`, `414`, `415`, `422`, or a `429` `DAILY` | Retry |
| `401` | `providerRejected`, `AUTHENTICATION`, after one OAuth refresh and resend only when the stored token has expired |
| `403` `MISSING_SCOPES` or other `403` | `providerRejected`, `AUTHORIZATION` |
| `404` `OBJECT_NOT_FOUND` | `notFound` on `getObject` and `updateObject`; `providerRejected` otherwise |
| `409` `CONFLICT` | Retry on `upsertObject` after one resend; `conflict` on `updateObject`; `providerRejected` on reads |
| a 2xx batch upsert error with a conflict | `conflict`, after one resend |
| `429` with policy `DAILY` | `providerRejected`, `QUOTA_EXHAUSTED` |
| other `429` (`RATE_LIMITS`) | Retry after `Retry-After`, or HubSpot's rate-limit interval up to 10 seconds |
| `423` Locked | Retry after at least 2 seconds |
| `408`, `477`, `5xx` except `501`, dropped connection | Retry, honoring `Retry-After` up to one hour |
| oversized, malformed, or ID-less response | `invalidResponse` |
| OAuth refresh rejected, or connection marked `reauthorization_required` | `providerRejected`, `AUTHENTICATION`, no request |
| OAuth token endpoint temporarily unavailable | Retry, no request |
| invalid input, or missing or malformed credentials | `defect`, no request |

Failure messages name only an allowlisted HubSpot category and an uppercase
error code, such as `(VALIDATION_ERROR, PROPERTY_DOESNT_EXIST)`. Receipts carry
the `X-HubSpot-Correlation-Id`. Only each operation's happy branch is
required; an unwired optional branch fails the Flow when selected.

Neither Mutation declares `uncertain`. Both writes are safe to send twice, so a
response that never arrives is retried under the Step policy. Every operation
uses asynchronous Execute durability, a 30-second Execute timeout, and unlimited attempts within one hour. HubSpot usually answers in well under a second;
the example's real-Dex test loses accepted provider answers and proves that the
retried upsert and update converge on one record.

## crmObjectChanged Trigger

`crmObjectChanged` polls one object type through
[CRM search](https://developers.hubspot.com/docs/api/crm/search) and delivers
one `hubspot.CRMObjectChangedEvent` per created or changed record. It is built
on the Connector SDK's durable poll Trigger kit
([`sdkgo/pollingtrigger`](../../sdkgo/README.md#polling-triggers), sdkgo
v0.38.0). It needs no HubSpot webhook setup, no public URL, and no scope beyond
the `crm.objects.<type>.read` scopes the authorization guide already lists.

- **Baseline.** The first poll of a binding reads the newest last-modified
  date of the object type (one search sorted descending, limit 1). Records
  modified at or before it are never delivered. An object type with no records
  delivers every later record.
- **Changes.** Each later poll sends `POST /crm/objects/2026-09/{objectType}/search`
  filtered on the last-modified property `GTE` the cursor, sorted ascending by
  it, 100 records per page, with the configured properties plus `createdate`
  and the last-modified property. The property is `lastmodifieddate` for
  contacts and `hs_lastmodifieddate` for companies and deals. Several edits
  between two polls arrive as one change with the record's latest values.
- **Cursor.** The cursor holds the baseline time, the newest delivered
  modification time, the next search anchor, and the delivered version of each
  record modified in the last two minutes. HubSpot says new and updated records
  can take a few moments to appear in search, so a caught-up poll re-reads that
  window and skips the versions it already delivered. The cursor keeps at most
  1,500 such versions; after a larger burst, such as an import, the re-read
  starts after the oldest version it had to drop, so a record of that burst
  indexed more than a few moments late can be missed. A full page moves the
  anchor to the page's newest time instead of paging by offset, because an edit
  during paging would shift offsets and skip a record. Only when more than a
  page of records share one millisecond does the cursor page with `after`.
  HubSpot reaches at most 10,000 results per query and answers 400 past that;
  a rejected paging offset, an undecodable cursor, or a cursor for another
  object type records a new baseline and logs a WARN
  `poll cursor invalid; reseeding` record. Changes in the gap are not delivered.
- **Change kind.** HubSpot reports no change type. `created` means HubSpot
  created the record after the newest change an earlier poll observed, so no
  earlier delivery or baseline included it. Every other change is `updated`,
  including a record that appeared in search only after the indexing delay.
- **Not reported.** CRM search returns no deleted, archived, or merged-away
  records, so the Trigger cannot report them.
- **Rate limits.** HubSpot limits search to five requests per second per
  account, and every search counts toward the account's daily limit. One
  runner sends one request at a time. A 429, or a 423 during a high-volume
  sync, pauses every binding of the connection for `Retry-After` or
  `X-HubSpot-RateLimit-Interval-Milliseconds`, at least one poll interval. An
  exhausted daily limit (`policyName: DAILY`) backs off exponentially up to 15
  minutes. Other failures back off per binding. None of them moves the cursor.
- **Delivery.** Delivery is at least once. The Trigger event ID is a hash of
  the object type, record ID, and last-modified millisecond, so a refetch yields
  the same ID and a Flow target keyed by it starts one Flow per record version.
- **Poll interval.** The connection's `pollInterval` setting is the delay
  between polls, at least 5 seconds.

`hubspot.NewProjectCRMObjectTriggerRunner` is the durable path. It loads each
binding's saved object type, records every change in the binding's project
Trigger inbox, and commits the cursor in the binding's project poll cursor only
after that, so a restarted or replaced replica neither misses nor repeats a
change. From
[`examples/crm-object-changed/main.go`](go/examples/crm-object-changed/main.go):

```go
triggerRunner, err := hubspot.NewProjectCRMObjectTriggerRunner(project, crmobjectchanged.ConnectionName, hubspot.ProjectCRMObjectTriggerRunnerConfig{
	CRMObjectChangedRoutes: []hubspot.ProjectCRMObjectChangedTriggerRoute{{
		BindingName: crmobjectchanged.CRMObjectChangedTriggerBinding, Properties: crmobjectchanged.DeliveredProperties,
		Target: sdkgo.NewDexFlowTriggerTarget(client, flow, crmobjectchanged.AcceptEveryCRMObjectChange, crmobjectchanged.ResolveFlowID,
			crmobjectchanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
	}},
}, hubspot.WithLogger(logger))
```

`hubspot.NewCRMObjectTriggerRunner` takes the targets and cursor stores
directly. A route without a cursor store keeps the cursor in memory. The
generated `hubspot.NewCRMObjectChangedTrigger` factory always does, because it
receives no project storage. Such a binding records a new baseline on every
process start, so changes made while the process is stopped are lost. Use them
only for tests and demonstrations.

The binding configuration is `hubspot.CRMObjectChangedTriggerConfiguration`:
the `objectTypePicker` unit saves `objectType`, which is required. The
application sets `Properties` in code; empty returns HubSpot's default search
properties. Changing the object type of a binding that already polled makes
its next poll record a new baseline. Restart the Worker after saving.

HubSpot webhooks would deliver changes sooner, but a private app's webhook
subscriptions can only be set in HubSpot's app settings, and a public app's
subscription API needs a developer API key this connector does not hold, so
they are not implemented.

## Studio units

| Unit | Output | Source |
| --- | --- | --- |
| `objectTypePicker` | `objectType` | the fixed `contacts`, `companies`, and `deals` names; also the `crmObjectChanged` binding's picker |
| `ownerPicker` | `ownerId` | `GET /crm/owners/2026-09`, paged with `after` |
| `dealStagePicker` | `pipelineId`, `stageId` | `GET /crm/pipelines/2026-09/deals` |

The Dex Web broker sends each read-only command with the connection's
`access_token` as a bearer credential; the bundle never receives it. When a
list cannot load, each unit offers a manual ID field.

## Project configuration

Dex Web or Superverse Studio saves the connection's settings and credentials in
the project configuration. The application loads that configuration once and
opens the connection by the name it declares, as
[`examples/lead-qualification/main.go`](go/examples/lead-qualification/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
settings, err := loadSettings(project.Configuration)
if err != nil {
	return err
}
connection, err := hubspot.NewProjectConnection(project, leadqualification.ConnectionName)
```

`LoadFromEnvironment` reads the `DEX_PROJECT_*` environment described in
[project configuration](../../sdkgo/projectconfig/README.md). Credentials stay
in project storage and are read for every call. An OAuth connection also
stores `oauth_client_id`, `oauth_client_secret`, `refresh_token`, and the
access token's expiry; when the token is missing, has no recorded expiry, or
expires within five minutes, the connector refreshes it and project storage
replaces it atomically.

## Connector mocks

`hubspotmock.New` provides a typed connection with an unscripted happy-path
answer for every operation. The default search contains three sorted pages:
the newest open deal, a repeated result at the page boundary, and an older
deal that the example leaves untouched. Named cases cover missing records,
conflicts, and conclusive provider rejections.

The defaults answer every request with one CRM: contact `501`
(`ada@example.com`), whose most recently modified open deal is `9001` in the
`default` pipeline. The `getObject` and `updateObject` answers report that deal
at stage `qualifiedtobuy`, the stage the integration test configures. The
example fails a deal whose read-back stage differs from its configured stage,
so an application configured with another stage needs a `getObject` script that
reads the deal back at that stage, for example `mock.GetObject().Default(...)`.
Script `updateObject` the same way so that both report one stage. Other
contacts, deals, or pipelines need scripts for the answers that name them. See
[`sdkgo/connectormock`](../../sdkgo/README.md#connector-mocks).

The [runnable integration test](go/examples/lead-qualification/flow/workflow_mock_integration_test.go)
uses these defaults on a real Dex Worker without provider credentials:

```go
mock := hubspotmock.New(t, ConnectionName)
harness := newHarnessForConnection(t, mock.Connection())
ctx := integrationContext(t, time.Minute)
flowID := startQualification(t, ctx, harness, "mock-default", Input{Email: existingLeadEmail, FirstName: "Ada", LastName: "Lovelace", Company: "Analytical Engines"})
qualification := waitForCompletedQualification(t, ctx, harness, flowID)
require.Equal(t, PhaseDealAdvanced, qualification.Phase)
```

## Example

[`examples/lead-qualification`](go/examples/lead-qualification) upserts a lead
contact by email, searches the contact's open deal in the configured pipeline,
moves it to the configured stage, and reads it back. It composes the
`ownerPicker` and `dealStagePicker` units.

[`examples/crm-object-changed`](go/examples/crm-object-changed) starts one Flow
per created or changed record of the picked object type through the
`crmObjectChanged` Trigger and reads the record back. It composes the
`objectTypePicker` unit on its Trigger binding.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/lead-qualification/flow/... -count=1 -v
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/crm-object-changed/... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration tests need a running `dexcli dev` and use a local fake
HubSpot API; no live HubSpot account is required.

## Python package

The [Python package](python/README.md) implements the four CRM operations in this manifest and, from `v0.2.0`, the `crmObjectChanged` Trigger at parity with Go, on the Python poll Trigger kit. Its cursor documents and event IDs match the Go connector's byte for byte, so Go and Python replicas can share one binding's cursor and inbox. Its generated connection supports private app tokens and manifest-driven HubSpot OAuth refresh; the [read contact example](python/examples/read_contact/README.md) binds the static `hubspot-crm` connection and includes a real Dex Worker and Client integration test, and the [crm_object_changed example](python/examples/crm_object_changed/README.md) starts one Flow per changed record through a real Dex Worker and Client. Go-recorded [conformance scenarios](../../conformance/connectors/hubspot/scenarios.json) cover provider requests, branches, and retries. Live HubSpot authorization and CRM calls remain unverified until a user configures a provider account in Dex Web.

| Capability | Kind | Languages |
| --- | --- | --- |
| `searchObjects`, `getObject`, `upsertObject`, `updateObject` | Operations | Go, Python, TypeScript |
| `crmObjectChanged` | Trigger | Go, Python |

## TypeScript package

The TypeScript package in [`typescript/`](typescript/README.md) implements the four CRM operations with the generated manifest OAuth refresh wiring and, from v0.2.0, the `crmObjectChanged` Trigger at parity with Go, on the TypeScript poll Trigger kit. Its cursor documents match the Go connector's byte for byte, so Go and TypeScript replicas can share one binding's cursor and inbox. Its [search deals example](typescript/examples/search-deals/README.md) runs a Dex Worker and Client using the static `hubspot-crm` connection, and its [crm-object-changed example](typescript/examples/crm-object-changed/README.md) starts one Flow per changed record. Provider replay and real local Dex integration tests cover the public path; live HubSpot authorization and API behavior still require testing through Dex Web Connections.

| Capability | Kind | Languages |
| --- | --- | --- |
| `searchObjects`, `getObject`, `upsertObject`, `updateObject` | Operations | Go, Python, TypeScript |
| `crmObjectChanged` | Trigger | Go, TypeScript |
