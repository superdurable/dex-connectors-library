# HubSpot Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for HubSpot; no live HubSpot account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

This module searches, reads, upserts, and updates HubSpot CRM contacts,
companies, and deals through HubSpot's 2026-09 CRM object API. It is the
system-of-record write path for a Flow: identify a person by email, find the
deal that belongs to them, and move that deal forward.

| Operation | Kind | HubSpot endpoint | Happy branch |
| --- | --- | --- | --- |
| `searchObjects` | Query | `POST /crm/objects/2026-09/{objectType}/search` | `searched` |
| `getObject` | Query | `GET /crm/objects/2026-09/{objectType}/{recordId}` | `found` |
| `upsertObject` | Mutation | `POST /crm/objects/2026-09/{objectType}/batch/upsert` with one input | `upserted` |
| `updateObject` | Mutation | `PATCH /crm/objects/2026-09/{objectType}/{recordId}` | `updated` |

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
After HubSpot rejects an unexpired OAuth access token, the connector refreshes
once and resends the request once. The older `/oauth/v1/token` endpoint is
deprecated on 2027-02-16, and this connector never calls it.

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

If HubSpot answers `409 CONFLICT`, the connector resends the same upsert once.
A concurrent dispatch of the same upsert may have created the record first, and
the resend updates it. A second conflict selects `conflict`, for example when
the properties contain an email that another contact already owns.

### updateObject

`UpdateObjectInput` sets named property values on one record ID; an empty
string clears a value. Repeating an update sets the same values again. The
repeat overwrites a change another user made to the same property in between,
as any last-write-wins retry does.

## Branches, errors, and retries

| HubSpot response | Result |
| --- | --- |
| `400`/`422` or `VALIDATION_ERROR` | `providerRejected`, `VALIDATION` |
| `401` | `providerRejected`, `AUTHENTICATION`, after one OAuth refresh |
| `403` `MISSING_SCOPES` or other `403` | `providerRejected`, `AUTHORIZATION` |
| `404` `OBJECT_NOT_FOUND` | `notFound` on `getObject` and `updateObject`; `providerRejected` otherwise |
| `409` `CONFLICT` | `conflict` on both Mutations, after one resend for `upsertObject` |
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
uses asynchronous Execute durability, a 30-second Execute timeout, and five
attempts within two minutes. HubSpot usually answers in well under a second;
when a write takes longer than Dex's roughly seven-second local phase, Dex
dispatches it again, and the example's real-Dex test proves that the repeated
upsert and update converge on one record.

## Studio units

| Unit | Output | Source |
| --- | --- | --- |
| `objectTypePicker` | `objectType` | the fixed `contacts`, `companies`, and `deals` names |
| `ownerPicker` | `ownerId` | `GET /crm/owners/2026-09`, paged with `after` |
| `dealStagePicker` | `pipelineId`, `stageId` | `GET /crm/pipelines/2026-09/deals` |

The Dex Web broker sends each read-only command with the connection's
`access_token` as a bearer credential; the bundle never receives it. When a
list cannot load, each unit offers a manual ID field.

## Local configuration

```json
{
  "schemaVersion": "connectors.dex.dev/local-connections/v1alpha1",
  "connections": [{
    "connectorId": "hubspot",
    "modulePath": "github.com/superdurable/dex-connectors-library/connectors/hubspot",
    "moduleVersion": "v0.1.0",
    "provider": "hubspot",
    "connectionName": "hubspot-crm",
    "configuration": {},
    "credentials": {"auth_method": "private-app-token", "access_token": "pat-na1-..."}
  }]
}
```

Load the file with `localconfig.LoadFromEnvironment` and create the connection
with `hubspot.NewLocalConnection(store, "hubspot-crm")`. An OAuth connection
also stores `oauth_client_id`, `oauth_client_secret`, `refresh_token`, and a
record-level `credentialExpiresAt`; the local provider refreshes it under a
lock and replaces the file atomically.

Hosted applications resolve operation-scoped tokens with
`hostedconfig.NewCredentialProviderFromEnvironment(hubspot.ConnectorID, name,
hubspot.DecodeResolvedCredentialsJSON)`. The broker response holds only
`access_token` and an optional `auth_method`; a response without
`auth_method` is treated as a static token that is never refreshed after a
rejection.

## Example

[`examples/lead-qualification`](examples/lead-qualification) upserts a lead
contact by email, searches the contact's open deal in the configured pipeline,
moves it to the configured stage, and reads it back. It composes the
`ownerPicker` and `dealStagePicker` units.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  GOWORK=off go test -tags=integration ./examples/lead-qualification/flow/... -count=1 -v
(cd ui && npm ci && npm test && npm run build)
```

The integration tests need a running `dexcli dev` and use a local fake
HubSpot API; no live HubSpot account is required.
