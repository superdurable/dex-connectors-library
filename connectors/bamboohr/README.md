# BambooHR Connector

Moved from `github.com/superdurable/dex-connectors-library/connectors/bamboohr`.

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
uses a 30-second Execute timeout. Reads and `updateEmployee` use the one-hour
default retry budget. `addEmployee` keeps five attempts within five minutes
because BambooHR does not document whether a 503 means the add was not applied.
It uses sync durability for its dispatch fence; see
[Duplicate safety](#duplicate-safety).

The `employeeChanged` Trigger starts a Flow for each employee BambooHR lists
as inserted, updated, or deleted, polled with a durable cursor; see
[employeeChanged Trigger](#employeechanged-trigger).

Install this connector release with `go get github.com/superdurable/dex-connectors-library/connectors/bamboohr/go@v0.24.0`.
The module requires Connector Go SDK v0.38.0 for the durable poll Trigger kit
(`sdkgo/pollingtrigger`) and the shared single-dispatch fence.

## Python package

The independent `dex-connector-bamboohr` Python package `v0.2.0` implements all
six operations above with the same branch IDs and provider request behavior as
the Go connector. It includes generated typed inputs, outputs,
operation-specific Step factories, named project connections, and mock
outcomes. `addEmployee` uses the Python Connector SDK's single-dispatch fence
and sync Execute durability. Install it with `dex-connector-bamboohr==0.2.0`;
it requires `dex-connectors-sdk` 0.12.0 or newer.

The package also implements the `employeeChanged` Trigger on the Python poll
Trigger kit at behavior parity with Go: the same settle window, ordering,
change type filter, event IDs, rate-limit and cursor-reset handling, and a
cursor document that Go and Python replicas read and write byte for byte. The
[Python employee-changed example](python/examples/employee_changed/README.md)
starts one Flow per change and reads inserted and updated employees back.

The [Python employee lookup example](python/examples/get_employee/README.md)
uses the static `bamboohr` connection name, a Worker entrypoint, and a start
command. Its real Dex integration test uses an inert BambooHR stand-in and
checks retry and unwired optional-branch failure. Go-recorded provider
scenarios are replayed by the Python package. A live BambooHR account has not
been used for this Python port, so live authorization and provider behavior,
including live polling, remain unverified.

This release has no webhook Triggers, no field or employee pickers, and no
OAuth; see [Not in this release](#not-in-this-release).

## TypeScript package

The [TypeScript package](typescript/README.md) v0.2.0 implements all six
operations with generated Step factories and a hand-written provider client.
Install it with `@superdurable/connector-bamboohr@0.2.0`; it requires
`@superdurable/connectors-sdk` 0.12.0.
Its [runnable getEmployee Flow](typescript/examples/get-employee/) uses the
static `bamboohr-employees` connection name. Go-recorded conformance scenarios
cover provider requests, branches, retries, uncertainty, and duplicate safety;
the example also ran through a real Dex Worker and Client with a local provider
fixture. Live BambooHR behavior remains unverified.

The package also implements the `employeeChanged` Trigger on the TypeScript
poll Trigger kit at behavior parity with Go: the same settle window, ordering,
change type filter, event IDs, rate-limit and cursor-reset handling, and a
cursor document that Go and TypeScript replicas read and write byte for byte.
The [TypeScript employee-changed example](typescript/examples/employee-changed/)
starts one Flow per change and reads inserted and updated employees back; live
BambooHR polling has not been verified.

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

## Project configuration

Name the factory connection and open the same name from the project
configuration at application startup, as
[`examples/new-hire-onboarding/main.go`](go/examples/new-hire-onboarding/main.go)
does:

```go
project, err := projectconfig.LoadFromEnvironment(ctx)
if err != nil {
	return err
}
connection, err := bamboohr.NewProjectConnection(project, newhireonboarding.ConnectionName, connectionOptions()...)
```

Dex Web or Superverse Studio writes that configuration, and the application
reads it through the `DEX_PROJECT_*` environment described in
[project configuration loading](../../sdkgo/projectconfig/README.md#application-loading).
The stored credential holds exactly `api_key`; the connector resolves it for
every call and rejects anything else without repeating the value.

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
comes from the response's `id`, or from a usable `Location` header. The current
[Create Employee reference](https://documentation.bamboohr.com/reference/create-employee)
documents a 201 response body with a required string `id` and a `Location`
header whose web-app URL includes the employee ID; their live formats remain
unverified. BambooHR rejects an email another employee already holds with 409.

## employeeChanged Trigger

`employeeChanged` polls BambooHR's
[Get Changed Employee IDs](https://documentation.bamboohr.com/reference/get-changed-employee-ids)
list, `GET /api/v1/employees/changed?since=...`, the same history
`listEmployeeChanges` reads, and delivers one `bamboohr.EmployeeChangedEvent`
per changed employee: the employee ID, BambooHR's `Inserted`, `Updated`, or
`Deleted` action, and `lastChanged`. BambooHR returns an employee for a
change to any field, employment status, job information, or compensation, and
a termination is an `Updated` change. The event carries no field values, so a
Flow reads the employee with `getEmployee`, as the
[employee-changed example](go/examples/employee-changed) does. It uses the
connection's existing API key; no other permission is needed, and BambooHR
lists only employees the key's user can see.

The binding's configuration is `bamboohr.EmployeeChangedTriggerConfiguration`,
saved in Dex Web by the `changeTypePicker` unit: `changeType` `inserted`,
`updated`, or `deleted` is sent as BambooHR's `type` filter, and blank
delivers every change. The connector's Studio bundle has no provider commands;
the unit only saves the choice.

- **Cursor.** The durable poll cursor stores the last delivered change's
  `lastChanged` and employee ID, like `listEmployeeChanges`' `nextCursor`, and
  the change type it was read with. Each poll asks one second before the
  cursor, because BambooHR does not document whether `since` is inclusive, and
  delivers the changes after it oldest first, ordered by `lastChanged` and then
  numeric employee ID, so changes that share a second are neither skipped nor
  repeated.
- **Settle delay.** BambooHR stamps `lastChanged` in whole seconds and does not
  document when a change becomes visible, so a change in a second the cursor
  already passed, such as a lower employee ID in the same second, would be
  skipped. A poll therefore delivers only changes at least 30 seconds older
  than BambooHR's response `Date` header (the local clock without one); newer
  changes wait for a later poll. A change reaches a Flow 30 seconds to
  `pollInterval` plus 30 seconds after it is made, and a change that appears
  more than 30 seconds late is not delivered unless the employee changes again.
- **First poll.** The cursor starts at BambooHR's clock, read from the
  response `Date` header (the local clock when the header is missing), so no
  change before that second starts a Flow. A change stamped with that second
  itself is delivered, because it may have been made after the binding
  started.
- **Coalescing.** BambooHR reports only each employee's latest change, so
  several edits between two polls arrive as one event with the latest action.
  An employee added and then edited before the next poll arrives as `Updated`
  for a binding that delivers every change. BambooHR does not document whether
  `type=inserted` still lists such an employee, so a short `pollInterval`
  keeps that window small.
- **Event IDs.** `employee-<id>-<action>-<lastChanged Unix seconds>` is a pure
  function of the change, so a refetch after a crash yields the same ID, and a
  later change to the same employee yields a new one.
- **Paging.** BambooHR does not paginate this list. A poll delivers at most 500
  changes (`pollingtrigger.MaxChangesPerPage`) and reads the rest at once, up to
  the kit's ten pages per poll. The whole list since the cursor must fit
  `maxResponseBytes`; a larger one fails the poll with a message to raise it,
  and never skips changes.
- **Rate limits.** BambooHR publishes no numeric limit and answers 429 with
  `Retry-After` when it throttles, as its
  [technical overview](https://documentation.bamboohr.com/docs/api-details)
  describes. A 429, or a 503, pauses every binding of the connection for
  `max(Retry-After, pollInterval)`, or backs off exponentially without the
  header. Other failures, such as 401 or 403, back off per binding and are
  logged.
- **Reset.** BambooHR documents 400 for a `since` it cannot accept. A 400, an
  undecodable stored cursor, or a cursor read with another change type
  starts the binding again at the current time and logs
  `poll cursor invalid; reseeding` at WARN; changes in between are not
  delivered.

`pollInterval` on the connection sets the delay between polls, at least 10
seconds. Use the runners rather than the generated factory for a durable
cursor:

- `bamboohr.NewProjectEmployeeChangedTriggerRunner` opens the connection from
  the loaded project, loads each binding's saved change type, and records each
  change in the binding's project Trigger inbox before the cursor commits in
  its project poll cursor, so a restarted or replaced Worker neither misses
  nor repeats a change;
- `bamboohr.NewEmployeeChangedTriggerRunner` takes routes with their own
  targets, cursor stores, and an optional `RequestLimiter`;
- the generated `bamboohr.NewEmployeeChangedTrigger` keeps the cursor in
  memory, so every start begins at the current time.

`bamboohr.WithLogger` sends the runners' records to a logger. Records carry
the connector, connection, Trigger, and binding, never employee data, API
keys, or cursor tokens. The [employee-changed example](go/examples/employee-changed/main.go)
wires the project runner:

```go
triggerRunner, err := bamboohr.NewProjectEmployeeChangedTriggerRunner(project, employeechanged.ConnectionName, []bamboohr.ProjectEmployeeChangedTriggerRoute{{
	BindingName: employeechanged.EmployeeChangedTriggerBinding,
	Target: sdkgo.NewDexFlowTriggerTarget(client, flow, employeechanged.AcceptEveryEmployeeChange, employeechanged.ResolveFlowID,
		employeechanged.MapToFlowInput, sdkgo.WithTriggerLogger(logger)),
}}, connectionOptions(logger)...)
```

## Duplicate safety

BambooHR API v1 documents no idempotency key, so a repeated add is a second
employee. Dex can replay a Step after Worker loss or before a result is committed, so the operations
take these positions:

- **updateEmployee** is safe to repeat. Its write sets absolute plain values
  computed from a fresh read, and history-table fields are refused, so a
  second dispatch sends the same values or finds them applied. It keeps async
  durability, and every unconfirmed outcome, including a 5xx or a lost
  response, is retried; its manifest declares `repeatOnUnknown: safe`.
- **addEmployee** uses the shared single-dispatch fence with sync durability.
  Its attempt-numbered heartbeat allows another send only after the immediately
  prior attempt recorded `notSent` or `canResend`. A missing, stale, or legacy
  checkpoint fails closed. An accepted 2xx with an employee ID saves that ID;
  a later attempt reads `GET /employees/{id}` instead of posting again.
- A 429, 503, or connection failure proven to occur before a connection opened
  records `canResend` and returns Retry. BambooHR's
  [technical overview](https://documentation.bamboohr.com/docs/api-details)
  describes 429 as throttling and 503 as endpoint unavailability. A 429 without
  `Retry-After` also retries; the provider says the header should be present,
  so the missing-header case has not been verified live.
- After a 503 resend, a 409 may come from the first request; `providerRejected`
  cannot rule out that the employee already exists.
- A 500, 408, ambiguous transport failure, or accepted 2xx without a usable
  employee ID selects `uncertain`. The connector cannot associate an email or
  name lookup with its specific POST. The onboarding example looks up the hire
  by personal email; a missing or ambiguous result fails the Flow.

Keep `addEmployee` sync. Async local-phase heartbeats are not visible to later
attempts. A checkpoint lost before Dex stored it, or a crash before the
request left, can produce `uncertain` for an add BambooHR never received.

The reads are safe to repeat. Two Flows that add the same person concurrently
are not deduplicated by the connector; give each hire a stable Flow ID so Dex
rejects the second start.

## Errors

A non-2xx response never exposes BambooHR's `X-BambooHR-Error-Message`
header or any message field. A Failure repeats only a machine-readable code
from `error.code` first, then `code` if the first value is unusable, such as
`BambooHR rejected the request (HTTP 422) [BadRequest]`. A code that contains
the API key is dropped. An oversized error body contributes no code.

| Response | Reads and updateEmployee | addEmployee |
| --- | --- | --- |
| 400, 406, 422 | `providerRejected`, `VALIDATION` | `providerRejected`, `VALIDATION` |
| 401, 403 | `providerRejected`, `AUTHENTICATION` or `AUTHORIZATION` | same |
| 404 | `notFound` where declared, otherwise `providerRejected` | `providerRejected` |
| 409, other 4xx | `providerRejected` | `providerRejected` |
| 3xx | `providerRejected`, `PROTOCOL`; redirects are never followed | same |
| 429 with `Retry-After` | Retry after `Retry-After` | Retry after `Retry-After` |
| 429 without `Retry-After` | Retry | Retry |
| connection refused before sending | Retry | Retry |
| 503 | Retry, after `Retry-After` when present | Retry |
| 408, other 5xx, lost or unreadable response | Retry, after `Retry-After` when present | `uncertain` |
| oversized, malformed, or credential-reflecting 2xx | `invalidResponse` | `uncertain` |
| invalid input | `defect`, with no request | `defect`, with no request |
| reauthorization required or unusable connection credentials | `defect`, with no request | `defect` after a `notSent` checkpoint, with no request; Retry while reading back an earlier attempt's write |
| transient credential resolution failure | `defect`, with no request | Retry after a `notSent` checkpoint, with no request |

BambooHR returned 503 for rate limiting until September 16, 2026, and returns
429 with `Retry-After` since; 503 now means the API is unavailable. The
Receipt carries the Call ID and the employee ID; BambooHR documents no
request ID header.

## Mocks

`bamboohrmock` provides one consistent default world: Ava Nguyen is employee
140, while adding Sam Park creates employee 142. Each operation has a happy
default and named definite business outcomes. Script the result that matters
to an application test; the real-Dex onboarding test contains this setup:

```go
mock := bamboohrmock.New(t, ConnectionName)
mock.FindEmployeeByEmail().Respond(bamboohrmock.FindEmployeeByEmailNotFoundFailure())
```

The complete runnable test is
[`examples/new-hire-onboarding/flow/workflow_mock_integration_test.go`](go/examples/new-hire-onboarding/flow/workflow_mock_integration_test.go).

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

### Webhook Triggers

BambooHR signs webhooks: each POST carries `X-BambooHR-Timestamp` and
`X-BambooHR-Signature`, an HMAC-SHA256 over the raw body followed by the
timestamp, keyed by a private key. Global webhooks are registered by an
administrator in the BambooHR settings, which generate the key; permissioned
webhooks are registered with `POST /api/v1/webhooks`, which returns the key
once. There is no handshake. The connector declares no webhook Trigger, because
a delivery has no provider event ID, batches several employees into one POST,
names fields by the labels the webhook's creator chose, and BambooHR documents
neither the timestamp format nor a replay window. Until a webhook source can
derive a stable per-employee event ID, use the poll-based
[employeeChanged Trigger](#employeechanged-trigger), or sweep with
`listEmployeeChanges` as the
[employee change sweep](go/examples/employee-change-sweep) example does.

### Studio pickers

A field or employee picker would list `GET /api/v1/meta/fields` or
`GET /api/v1/employees`, but Studio setup commands declare a fixed HTTPS host
and support only bearer or raw-header credentials. BambooHR's host is per
company and an API key needs HTTP Basic, so the connection uses guided inputs
and field names are operation input. The Studio bundle therefore declares no
provider commands; its only unit, `changeTypePicker`, saves the
`employeeChanged` change type.

### History tables and termination

Job information, compensation, and employment status changes, including a
termination, are written through BambooHR's table endpoints, which append
rows. This release reads those values through `getEmployee` and detects
terminations through `listEmployeeChanges`, but does not write them.

## Examples

- [`examples/new-hire-onboarding`](go/examples/new-hire-onboarding) is a runnable
  Dex Web **Start Flow** example that uses five operations: it finds a new hire
  by personal email or adds them, checks that the record holds what IT needs,
  lists time off in the first two weeks, and records the provisioning hand-off
  in a custom field.
- [`examples/employee-change-sweep`](go/examples/employee-change-sweep) is a
  runnable **Start Flow** example that pages through `listEmployeeChanges`
  from a cursor and completes with the cursor the next sweep starts from.
- [`examples/employee-changed`](go/examples/employee-changed) is a runnable
  Trigger example: the `employeeChanged` binding, configured with the
  `changeTypePicker` unit, starts one Flow per changed employee, which reads
  the employee back with `getEmployee`.
- [`python/examples/employee_changed`](python/examples/employee_changed) is the
  same Trigger example in Python, with the same binding, configuration unit,
  and read-back.
- [`typescript/examples/employee-changed`](typescript/examples/employee-changed)
  is the same Trigger example in TypeScript, with the same binding,
  configuration unit, and read-back.

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
credential-reflecting responses, and invalid credentials. For
`employeeChanged`, the `internal/bamboohrfake` stand-in, which stamps changes
with its own clock and `Date` header, covers a seed against a clock behind the
local one, the settle delay with a change that becomes visible late in a second
the list already showed, the `type` filter, more than 500 changes, 429 and 503 pauses with
`Retry-After`, the 400 reset, undecodable cursors, and an oversized list. The
example's real Dex tests cover delivery across a restart and the reset.

No live BambooHR account was used. The following live behavior is
unverified: the API Keys menu path and key removal in the setup guidance;
whether `since` on the change history is inclusive and whether its empty
history is `{}` or `[]`; whether a rate-limited add is never applied; whether
the time off filter matches lowercase statuses and supports `in`; whether
`supervisorEId` is readable through Get Employee as the field list documents;
whether a field requested by numeric ID returns under its name; whether
duplicate-email 409 covers `homeEmail`; whether List Employees includes a hire
whose start date is in the future; and the live 201 body ID and `Location`
header formats, 503 non-application, and 429 without `Retry-After`. For
`employeeChanged`: whether the live `Date` header tracks the clock BambooHR
stamps `lastChanged` with, whether `type=inserted` still lists an employee
edited after the insert, how quickly a change appears in the list, and the
Dex Web rendering of the `changeTypePicker` unit with a real connection.
