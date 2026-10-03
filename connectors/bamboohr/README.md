# BambooHR Connector

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for BambooHR; no live BambooHR account was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The BambooHR Connector reads and writes the BambooHR person record from Dex
Flows through BambooHR API v1. It exposes these operation-specific Dex Step
factories:

| Operation | Kind | Durability | Happy branch | Other branches |
| --- | --- | --- | --- | --- |
| `bamboohr.NewGetEmployeeStep` | Query | async | `found` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `bamboohr.NewFindEmployeeByEmailStep` | Query | async | `found` | `notFound`, `ambiguous`, `providerRejected`, `invalidResponse`, `defect` |
| `bamboohr.NewListEmployeeChangesStep` | Query | async | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `bamboohr.NewListTimeOffRequestsStep` | Query | async | `listed` | `providerRejected`, `invalidResponse`, `defect` |
| `bamboohr.NewUpdateEmployeeStep` | Mutation | async | `updated` | `notFound`, `providerRejected`, `invalidResponse`, `defect` |
| `bamboohr.NewAddEmployeeStep` | Mutation | sync | `created` | `providerRejected`, `uncertain`, `defect` |

Only the happy-path branch of each operation is required; every other branch
is optional, and an unwired optional branch fails the Flow. Every operation
uses a 30-second Execute timeout and a five-minute retry window, which fits
BambooHR's `Retry-After` on a rate-limited request. `addEmployee` uses sync
durability because BambooHR has no idempotency key; see
[Duplicate safety](#duplicate-safety).

This release has no Triggers, no Studio pickers, and no OAuth; see
[Not in this release](#not-in-this-release).

## BambooHR setup

A connection needs two values:

- **companyDomain**, non-secret configuration: the `mycompany` in
  `https://mycompany.bamboohr.com`. Every request goes to
  `https://{companyDomain}.bamboohr.com/api/v1`, the form BambooHR has
  documented since its July 2025 routing change.
- **api_key**, a secret credential field: an API key of the BambooHR user the
  connector acts as. Every request is permissioned as that user, so the user's
  access level decides which employees, fields, and time off requests the
  connector can read and change.

The user signs in, selects their name in the lower left-hand corner of any
page, and chooses **API Keys**, as BambooHR's
[getting-started guide](https://documentation.bamboohr.com/docs/getting-started)
describes; the option appears only when the access level allows API keys.
BambooHR shows a new key once. The connector authenticates with HTTP Basic,
the key as the user name and `x` as the password, and sends the credentials on
every request rather than waiting for BambooHR's 401 challenge, as BambooHR
recommends. After repeated requests with an unknown key, BambooHR answers 403
to every request for a period.

Credentials are reread before every provider call, so replacing a key in Dex
Web takes effect without a restart. The company domain and response limit are
startup configuration.

## Local configuration

Dex Web writes this record for the connection name the examples use:

```json
{
  "connectorId": "bamboohr",
  "modulePath": "github.com/superdurable/dex-connectors-library/connectors/bamboohr",
  "moduleVersion": "v0.1.0",
  "provider": "bamboohr",
  "connectionName": "bamboohr-company",
  "configuration": {"companyDomain": "mycompany"},
  "credentials": {"api_key": "..."}
}
```

Load it with `localconfig.LoadFromEnvironment` and
`bamboohr.NewLocalConnection`, as
[`examples/new-hire-onboarding/main.go`](examples/new-hire-onboarding/main.go)
does:

```go
store, err := localconfig.LoadFromEnvironment()
if err != nil {
	return err
}
connection, err := bamboohr.NewLocalConnection(store, newhireonboarding.ConnectionName, connectionOptions()...)
```

## Hosted credentials

In Superverse-hosted deployments, construct the client with the
operation-scoped broker provider. `DecodeResolvedCredentialsJSON` accepts
exactly `api_key` and rejects anything else without repeating the value.

## BambooHR's own vocabulary

The connector passes BambooHR's names and values through and never maps them
to another vocabulary:

- Field names are BambooHR's, such as `workEmail`, `hireDate`, or a
  custom-field alias such as `customStartDate`. Values are BambooHR's text:
  dates are `YYYY-MM-DD`, and list fields hold the exact option text.
- An employee's `status` is `Active` or `Inactive`; the List Employees filter
  writes it in lowercase (`bamboohr.EmployeeStatusFilterActive`).
- A change action is `Inserted`, `Updated`, or `Deleted`; the change filter
  writes it in lowercase (`bamboohr.EmployeeChangeTypeInserted`). A
  termination is an `Updated` change, not a `Deleted` one.
- A time off request status is `REQUESTED`, `APPROVED`, `DENIED`, or
  `CANCELED` (`bamboohr.TimeOffRequestStatuses()`), and its unit is `HOURS` or
  `DAYS`.

Employee IDs are BambooHR's internal IDs, digit strings such as `123`. They
are not the editable Employee # (`employeeNumber`), and the caller sentinel
`0` is not accepted.

## Operations

### getEmployee

`getEmployee` reads `GET /api/v1/employees/{id}?fields=...` with 1 to 400
fields (`bamboohr.MaxRequestedFields`), comma-separated, the only form
BambooHR accepts. Fields may be standard names, custom-field aliases, or the
numeric field IDs from BambooHR's List Fields endpoint. BambooHR returns
nothing but the ID when no field is requested, so at least one is required.
`includeFutureValues` sends `onlyCurrent=false` to include future-dated values
from history tables.

BambooHR silently omits a field that is unknown or that the connection's user
cannot view, so the record lists those as `omittedFields`; an omitted field is
never reported as empty. A null value is an empty string, a boolean is `true`
or `false`, and a structured value is its compact JSON. A missing employee
selects `notFound`; an employee the user cannot view returns 403, which
selects `providerRejected`.

### findEmployeeByEmail

`findEmployeeByEmail` reads one page of
`GET /api/v1/employees?filter[workEmail]=...` (or `filter[homeEmail]`), the
List Employees endpoint BambooHR added in October 2025, sorted by employee ID
with `page[limit]=100` (`bamboohr.MaxEmailCandidates`). BambooHR's email
filter is a case-insensitive substring match, so the connector keeps only the
employees whose address equals the requested one, ignoring letter case:
`ava@acme.example.com` never matches `ava@acme.example.com.au`.

- One match selects `found` with the employee's ID, names, job title, status,
  both emails, and up to 20 additional requested fields.
- No match selects `notFound`. BambooHR drops an employee whose email the
  connection cannot read, so `notFound` means no employee visible to the
  connection has the address.
- Several matches, or a page that does not hold every candidate, selects
  `ambiguous` with the matches it read.

`status` optionally keeps only active or inactive employees. `homeEmail` is
often the only address a new hire has before IT issues a work email. The
company directory was not chosen because it returns the whole directory with
no filter and excludes inactive and unpublished employees; custom reports are
deprecated.

### listEmployeeChanges

`listEmployeeChanges` reads `GET /api/v1/employees/changed?since=...`, the
change history BambooHR keeps for every field, employment status, job
information, and compensation change, and is the poll source for onboarding
(`changeType: inserted`) and offboarding (`updated`). BambooHR returns every
employee changed since the instant with no pagination, each with only its
latest change, so the connector bounds the result itself:

- it returns at most `limit` changes (1 to 1000, 100 by default), oldest first,
  ordered by `lastChanged` and then numeric employee ID;
- `nextCursor` holds the last returned change's instant and employee ID, and
  `hasMore` reports that more changes remain, so poll again at once;
- pass `nextCursor.since` and `nextCursor.afterEmployeeId` back to continue
  without gaps or duplicates, even when several employees changed in the same
  second.

BambooHR does not document whether `since` is inclusive, so the connector asks
one second early and filters locally. The whole history since the cursor must
fit `maxResponseBytes`; a cursor far in the past in a large company selects
`invalidResponse`, so poll regularly or raise the limit.

### listTimeOffRequests

`listTimeOffRequests` reads one page of `GET /api/v1/time-off/requests`, the
paginated endpoint, with an OData filter for requests that overlap the window:
`startDate le '{endDate}' and endDate ge '{startDate}'`, optionally
`employeeId eq {id}` and `status in ('approved', 'requested')`.
`bamboohr.BuildTimeOffRequestFilter` returns the filter for review. A page
holds 1 to 200 requests (`bamboohr.MaxTimeOffPageSize`; BambooHR allows
1000), and `nextPage` is zero after the last page. A request a later edit
replaced is never returned.

Employee and manager notes are not returned, so free text about an absence
never enters Step state. BambooHR returns an empty page, not an error, for an
employee whose time off the connection cannot view.

### updateEmployee

`updateEmployee` sets up to 50 fields of one employee. It reads the requested
fields, sends `POST /api/v1/employees/{id}` with only the fields that differ,
then reads them back:

- `writtenFields` lists the fields this attempt sent; when nothing differs, it
  selects `updated` with `wasAlreadyApplied: true` and writes nothing, so an
  unchanged value never moves the employee's last-changed time and never
  re-triggers a change poll;
- `unmatchedFields` lists fields whose stored value differs from the requested
  text after the write: BambooHR rewrites some values, such as a state name to
  its abbreviation, and silently drops a field the connection cannot see or
  edit.

It refuses, as `defect`, fields BambooHR keeps in its job information,
compensation, or employment status history tables (`jobTitle`, `department`,
`division`, `location`, `reportsTo`, `payRate`, `payType`, `payPer`,
`paidPer`, `paySchedule`, `overtimeRate`, `exempt`, `employmentHistoryStatus`,
`employmentStatus`, `employeeStatusDate`, `employmentType`,
`terminationDate`), numeric field IDs, `id`, and photo keys. BambooHR
documents that writing `jobTitle` creates a position-history row and writing
`employmentHistoryStatus` appends an employment status record, so a repeated
attempt could append a second row. A duplicate email or an invalid value
returns 409, which selects `providerRejected`.

### addEmployee

`addEmployee` sends `POST /api/v1/employees` with the first and last name,
which BambooHR requires, and optional preferred name, work and home email,
hire date, and up to 50 other writable fields, such as `department` or
`employmentHistoryStatus` for the first employment status record. The new ID
comes from the response's `id`, or from the `Location` header BambooHR
documents. BambooHR rejects an email another employee already holds with 409.

## Duplicate safety

BambooHR API v1 documents no idempotency key, so a repeated add is a second
employee. Dex re-dispatches an async Step whose local attempt passes about
seven seconds, and it retries a Step after a lost Worker, so the operations
take these positions:

- **updateEmployee** is safe to repeat. Its write sets absolute plain values
  computed from a fresh read, and history-table fields are refused, so a
  second dispatch sends the same values or finds them applied. It keeps async
  durability, and every unconfirmed outcome, including a 5xx or a lost
  response, is retried.
- **addEmployee** runs with sync durability, so Dex never sends a second
  attempt while the first is in flight. Before sending, the operation records
  a Dex heartbeat checkpoint naming the Step's Call ID. A later attempt of the
  same Step execution that finds the checkpoint, after a lost Worker or an
  Execute timeout, selects `uncertain` without sending.
- Only an outcome that proves BambooHR did not apply the add is retried: a 429
  with `Retry-After`, which BambooHR sends for its rate limit instead of
  processing the request, and a connection that failed before it opened. The
  checkpoint is cleared first. A 429 without `Retry-After` is BambooHR's
  employee limit and selects `providerRejected` with `QUOTA_EXHAUSTED`.
- A 5xx, a 408, a lost or unreadable response, and an invalid 2xx select
  `uncertain`: the employee may exist, and the connector never resends the
  add. The application decides; the onboarding example looks the hire up by
  personal email and continues with the employee it finds, or stops for a
  person.

Keep `addEmployee` sync. An application override to async durability lets
Dex dispatch a second add after seven seconds. A checkpoint lost before Dex
stored it, or a crash before the request left, can make an attempt report
`uncertain` for an add BambooHR never received; that direction is safe.

The reads are safe to repeat. Two Flows that add the same person concurrently
are not deduplicated by the connector; give each hire a stable Flow ID so Dex
rejects the second start.

## Errors

A non-2xx response never exposes BambooHR's `X-BambooHR-Error-Message`
header or any message field. A Failure repeats only a machine-readable code
from `error.code` or `code`, such as
`BambooHR rejected the request (HTTP 422) [BadRequest]`. A code that contains
the API key is dropped.

| Response | Reads and updateEmployee | addEmployee |
| --- | --- | --- |
| 400, 406, 422 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | `providerRejected` |
| 409, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 with `Retry-After` | Retry after `Retry-After` | Retry after `Retry-After` |
| 429 without `Retry-After` | Retry | `providerRejected`, `QUOTA_EXHAUSTED` |
| connection refused before sending | Retry | Retry |
| 408, 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input or connection credentials | `defect`, with no request | `defect`, with no request |

BambooHR returned 503 for rate limiting until September 16, 2026, and returns
429 with `Retry-After` since; 503 now means the API is unavailable. The
Receipt carries the Call ID and the employee ID; BambooHR documents no
request ID header.

## Not in this release

### OAuth and OpenID Connect

BambooHR's OAuth 2.0 for marketplace and developer-portal applications
authorizes at `https://{companyDomain}.bamboohr.com/authorize.php` and
exchanges codes at `https://{companyDomain}.bamboohr.com/token.php`, both per
company, and documents a JSON token request body. Manifest OAuth endpoints are
static URLs, so the connector cannot declare them, and Dex Web's code exchange
through `cli-v1.2.0` is form-encoded. BambooHR's older OpenID
Connect login, which exchanged an `id_token` for an API key, is closed to new
applications since April 2025. The connector therefore offers API keys only,
which suit a customer connecting their own company.

### Triggers

BambooHR signs webhooks: each POST carries `X-BambooHR-Timestamp` and
`X-BambooHR-Signature`, an HMAC-SHA256 over the raw body followed by the
timestamp, keyed by a private key. Global webhooks are registered by an
administrator in the BambooHR settings, which generate the key; permissioned
webhooks are registered with `POST /api/v1/webhooks`, which returns the key
once. There is no handshake. The connector still declares no Trigger, because
a delivery has no provider event ID, batches several employees into one POST,
names fields by the labels the webhook's creator chose, and BambooHR documents
neither the timestamp format nor a replay window. Until a webhook source can
derive a stable per-employee event ID, poll with `listEmployeeChanges`, as the
[employee change sweep](examples/employee-change-sweep) example does.

### Studio pickers

A field or employee picker would list `GET /api/v1/meta/fields` or
`GET /api/v1/employees`, but Studio setup commands declare a fixed HTTPS host
and support only bearer or raw-header credentials. BambooHR's host is per
company and an API key needs HTTP Basic, so the connection uses guided inputs
and field names are operation input.

### History tables and termination

Job information, compensation, and employment status changes, including a
termination, are written through BambooHR's table endpoints, which append
rows. This release reads those values through `getEmployee` and detects
terminations through `listEmployeeChanges`, but does not write them.

## Examples

- [`examples/new-hire-onboarding`](examples/new-hire-onboarding) is a runnable
  Dex Web **Start Flow** example that uses five operations: it finds a new hire
  by personal email or adds them, checks that the record holds what IT needs,
  lists time off in the first two weeks, and records the provisioning hand-off
  in a custom field.
- [`examples/employee-change-sweep`](examples/employee-change-sweep) is a
  runnable **Start Flow** example that pages through `listEmployeeChanges`
  from a cursor and completes with the cursor the next sweep starts from.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
```

With the latest Dex development server running, the examples own their real
Worker, retry, persistence, duplicate-dispatch, and transition coverage:

```bash
GOWORK=off go test -tags=integration ./... -count=1 -v
```

From the repository root, check the manifest and generated code:

```bash
go run ./cmd/connectorctl validate connectors/bamboohr/connector.yaml
go run ./cmd/connectorctl generate --check connectors/bamboohr/connector.yaml
```

The provider fakes cover Basic authentication, comma-separated fields and
omitted fields, the exact-match filter over BambooHR's substring search, the
change cursor across equal timestamps and an empty `[]` history, the OData
overlap filter and dropped notes, only-changed-field updates with read-back
and history-table refusal, the dispatch checkpoint and its clearing, the
`Location` fallback, 429 with and without `Retry-After`, BambooHR error codes
without message text, redirects, oversized, malformed, and
credential-reflecting responses, and invalid credentials.

No live BambooHR account was used. The following live behavior is
unverified: the API Keys menu path and key removal in the setup guidance;
whether `since` on the change history is inclusive and whether its empty
history is `{}` or `[]`; whether a rate-limited add is never applied; whether
the time off filter matches lowercase statuses and supports `in`; whether
`supervisorEId` is readable through Get Employee as the field list documents;
whether a field requested by numeric ID returns under its name; whether
duplicate-email 409 covers `homeEmail`; whether List Employees includes a hire
whose start date is in the future; and the `Location` header format.
