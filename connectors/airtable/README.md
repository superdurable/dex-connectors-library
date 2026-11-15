# Airtable Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/airtable/go@v0.24.0`.

The [TypeScript package](typescript/) is `@superdurable/connector-airtable`
v0.2.0. It implements the four record operations and the `recordChanged`
Trigger, and provides generated operation-specific Step factories and mocks.
Its [runnable list example](typescript/examples/list-records/) uses a static
`airtable-policies` Dex connection, and its
[record-changed example](typescript/examples/record-changed-intake/) uses
`airtable-intake`. Shared Go-recorded provider scenarios and a real Dex integration
test cover the package. Live Airtable authorization, picker behavior, and
provider requests remain unverified.

Moved from `github.com/superdurable/dex-connectors-library/connectors/airtable`.

> **Verification status: Dex-integrated, not live.** Ran on a real Dex stack against a local stand-in for Airtable; no live Airtable base was used. See
> [verification status](../../docs/verification-status.md) for what is and
> is not verified.

The Airtable Connector reads and writes records in Airtable bases through the
[Airtable Web API](https://airtable.com/developers/web/api/introduction). It
exposes four operation-specific Dex Step factories:

| Factory | Kind | Airtable request | Happy branch |
| --- | --- | --- | --- |
| `airtable.NewListRecordsStep` | Query | `POST /v0/{baseId}/{tableIdOrName}/listRecords` | `listed` |
| `airtable.NewGetRecordStep` | Query | `GET /v0/{baseId}/{tableIdOrName}/{recordId}` | `found` |
| `airtable.NewUpsertRecordsStep` | Mutation | `PATCH /v0/{baseId}/{tableIdOrName}` with `performUpsert` | `upserted` |
| `airtable.NewUpdateRecordsStep` | Mutation | `PATCH /v0/{baseId}/{tableIdOrName}` with record IDs | `updated` |

The Go module also ships the `recordChanged` poll Trigger, which starts a Flow
for each record created or changed in one picked table; see
[Triggers](#triggers). The Python and TypeScript packages implement it too.

| Capability | Go | Python | TypeScript |
| --- | --- | --- | --- |
| `listRecords`, `getRecord`, `upsertRecords`, `updateRecords` | yes | yes | yes |
| `recordChanged` Trigger | yes | yes | yes |

Airtable is a typed table store: fields have types, and linked record fields
hold the IDs of records in another table of the same base. The connector keeps
that shape instead of flattening cells to strings the way a spreadsheet does.
`CellValue` carries Airtable's JSON cell format. Write values with
`TextCellValue`, `NumberCellValue`, `CheckboxCellValue`,
`LinkedRecordsCellValue`, `MultipleSelectsCellValue`, and `NullCellValue`, and
read them with `Text`, `Number`, `Checkbox`, and `StringList`. Airtable omits
empty fields from every record it returns, so a missing key means an empty
field. A write rejects the zero `CellValue`, so a forgotten value never clears
a field.

The Python package is [`dex-connector-airtable`](python/README.md), imported as
`dex_connectors.airtable`.

## Mocks

The generated [`airtablemock`](go/airtablemock) package supplies one refund policy
and its approved decision log across listing, upsert, update, and read-back
operations. The listing default has three cursor pages; the example Flow reads
only the first. Cases also cover expired offsets, missing rows, and denied
operations. The [runnable integration test](go/examples/refund-decision/flow/workflow_mock_integration_test.go)
uses the example Flow:

```go
	mock := airtablemock.New(t, ConnectionName)
	harness := newRefundDecisionHarnessWithConnection(t, mock.Connection())
	ctx := integrationContext(t, time.Minute)
```

Inspect each operation's `Calls()` to verify the Flow's inputs and call count.
Mock answers do not perform Airtable requests.

## Authentication

A connection uses one Airtable personal access token (`airtable-personal-access-token`)
sent as `Authorization: Bearer`. Create it at
<https://airtable.com/create/tokens> with the scopes `data.records:read`,
`data.records:write`, and `schema.bases:read`, and add every base the Flows
use under **Access**. The token acts as the user who created it, so that user
needs editor access to those bases. The `recordChanged` Trigger needs no
further scope: it lists records with `data.records:read` and reads the table
schema with `schema.bases:read`. Regenerating or deleting the token rejects
the old value; paste the replacement into the connection, and the running
Worker uses it on its next call.

Airtable OAuth is not shipped. Airtable's token endpoint,
`https://airtable.com/oauth2/v1/token`, requires the client credentials as
HTTP Basic whenever the integration has a client secret and forbids them
otherwise, as both the
[OAuth reference](https://airtable.com/developers/web/api/oauth-reference) and
Airtable's official `oauth-example` show. Dex Web `cli-v1.2.0` sends
`client_id` and `client_secret` in the form body of the authorization code
exchange, with an empty secret when its field is optional, and has no
manifest setting for HTTP Basic. An integration with a secret would therefore
fail the exchange, and one without a secret would depend on Airtable ignoring
an empty `client_secret`, which its documentation does not say. Airtable
access tokens last 60 minutes, refresh tokens 60 days, and
every refresh rotates both; a later release can add an OAuth method that
refreshes with `sdkgo/oauthtoken` and `ClientSecretBasic` once Dex Web can
complete the exchange.

## Operations

### listRecords

`ListRecordsInput` selects one page of at most `PageSize` records, 1 to 100
(zero means 100), and returns `RecordPage.Offset` for the next page; a blank
offset means the last page. Every parameter travels in the JSON body of
Airtable's `POST listRecords` form, so a long formula never meets Airtable's
16,000-character URL limit.

`Filters` hold typed equality filters, built with `TextFieldEquals`,
`NumberFieldEquals`, and `CheckboxFieldEquals`. The connector writes each one
as an Airtable formula and joins them, and an optional raw `Formula`, with
`AND` into `filterByFormula`; `RecordPage.FilterFormula` reports what it sent.
Text is quoted with Airtable's documented `\"` escape. Airtable documents no
escape for a backslash, a line break, or a brace inside a `{field}` reference,
so a typed filter rejects those characters as `defect`; use the field ID, such
as `fldXXXXXXXXXXXXXX`, or a raw `Formula` instead. A raw formula is sent as
written, so build it from application code, never from end-user text.
`Fields` limits the returned fields, `Sort` orders records and overrides
`ViewIDOrName`'s order, and without either Airtable returns records in no
particular order.

When Airtable no longer recognizes an offset (`422
LIST_RECORDS_ITERATOR_NOT_AVAILABLE`), the operation selects the optional
`offsetExpired` branch, and the listing must restart from the first page.

### getRecord

`GetRecordInput` reads one record by record ID. A missing record selects
`notFound`. Airtable falls back to a base-wide lookup when the record is not
in the named table, so a record ID from another table of the same base is
still returned; the response does not name the table.

### upsertRecords

`UpsertRecordsInput` writes up to 10 records matched by one to three
`FieldsToMergeOn`. Airtable creates a record when no existing record matches
the record's values for those fields, updates the one record that matches,
and rejects the whole request when several match. Airtable lists number,
text, long text, single select, multiple select, and date fields as merge
fields, never computed ones such as formulas, lookups, or rollups. Every record holds
a non-null value for every merge field, and no two records in one request
share merge values.

Airtable has no idempotency key. The merge fields make the write repeat-safe:
a retry finds the record the first attempt created and updates it with the
same values, so the operation retries every ambiguous outcome and declares no
`uncertain` branch. `UpsertedRecords.CreatedRecordIDs` describes the answered
request; after a lost response, the record the first attempt created appears
in `UpdatedRecordIDs`.

Airtable does not enforce a unique merge key, and its documentation does not
say that concurrent upserts are serialized. A Worker lost during a request
can cause another Execute while the first request may still be in flight, so
two concurrent upserts could each find no match and both create a record. The
operation therefore defaults to sync durability. A residual race remains
when a Worker is lost mid-request and its retry reaches Airtable before the
abandoned request finishes; the next upsert of that key then selects
`providerRejected` for several matches instead of writing the wrong record.

No plain `createRecords` operation ships: without an idempotency key or a
merge key, a retried create cannot tell whether the first one landed. Give an
append-only log a stable key field, such as a case or event ID, and upsert it.

### updateRecords

`UpdateRecordsInput` sets the named fields of up to 10 records by record ID
and leaves every other field unchanged. Each value replaces the whole field,
including every link of a linked record field, so repeating an update is safe;
the operation keeps async durability, and a real-Dex test proves a slow update
dispatched twice converges. The request never sets `performUpsert`, so an
unknown record ID is never created; a `404` selects `notFound`.

### Branches and failures

| Airtable outcome | Branch or Retry | Failure kind |
| --- | --- | --- |
| `2xx` with a valid body | the happy branch | none |
| `401` | `providerRejected` | `AUTHENTICATION` |
| `403`, such as `INVALID_PERMISSIONS_OR_MODEL_NOT_FOUND` | `providerRejected` | `AUTHORIZATION` |
| `404` | `notFound` on `getRecord` and `updateRecords`, otherwise `providerRejected` | `NOT_FOUND` |
| `422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE` | `offsetExpired` on `listRecords` | `NOT_FOUND` |
| `400`, `413`, other `422` | `providerRejected` | `VALIDATION` |
| `429` | Retry after at least 30 seconds | `RATE_LIMIT` |
| `408`, `5xx` except `501`, a dropped connection | Retry, honoring `Retry-After` | `AVAILABILITY` or `TRANSPORT` |
| `3xx`, `501`, other `4xx` | `providerRejected` | `PROTOCOL` or `PROVIDER_REJECTION` |
| Malformed, inconsistent, or oversized `2xx` | `invalidResponse` | `PROTOCOL` or `RESPONSE_TOO_LARGE` |
| Invalid input or connection | `defect`, with no request | `VALIDATION` or `AUTHENTICATION` |

A Failure names only the HTTP status and Airtable's uppercase error type, such
as `INVALID_VALUE_FOR_COLUMN`, never Airtable's message text or the token. A
write that saved records but not every attachment (`partialSuccess`) still
selects its happy branch and lists the documented reasons in
`PartialSuccessReasons`.

### Rate limits

Airtable allows 5 requests per second per base and asks a rate-limited client
to wait 30 seconds. A `Client` spaces its requests to each base 200
milliseconds apart; a Step whose slot is more than two seconds away returns
Retry instead of waiting. After a `429`, the client holds that base for 30
seconds, so other Steps in the same process do not extend the penalty. Other
processes that share a base are not coordinated. Every operation's retry
policy allows three minutes, which fits several 30-second waits.

## Studio configuration

The `ui/` bundle provides three release-owned units:

- `basePicker` lists the bases the token can reach through the read-only
  `listBases` command, `GET https://api.airtable.com/v0/meta/bases`, and stores
  `baseId` and `baseName`. It follows Airtable's `offset` cursor for up to 20
  pages of 1,000 bases and offers manual base ID entry beyond that.
- `tablePicker` reads the saved `baseId` and lists that base's tables through
  `listTables`, `GET https://api.airtable.com/v0/meta/bases/{baseId}/tables`,
  and stores the stable `tableId`, which keeps working after a rename, and
  `tableName`.
- `lastModifiedTimeFieldPicker` reads the saved `baseId` and `tableId`, lists
  the table's Last modified time fields that include a time of day from the
  same `listTables` response, and stores the stable `fieldId` and `fieldName`.
  The `recordChanged` Trigger binding uses it; a table without such a field
  shows how to add one.

Both commands send the token as a bearer credential that Dex Web injects; the
iframe never receives it. Dex Web bounds a command response at 4 MiB, so a
base whose schema is larger cannot list its tables, and the unit falls back to
manual table ID entry. The commands always call Airtable's public API host,
whatever the connection's `endpoint`.

| Connection field | Default | Purpose |
| --- | --- | --- |
| `endpoint` | `https://api.airtable.com` | Airtable Web API base URL for operations. |
| `maxResponseBytes` | `4194304` | Largest complete response; larger ones select `invalidResponse`. |
| `pollInterval` | `30s` | Delay between `recordChanged` polls of each binding, at least `1s`. |
| `pollPageSize` | `100` | Records one `recordChanged` poll request reads, 1 to 100. |
| `personal_access_token` | none | Secret personal access token. |

## Triggers

### recordChanged

`recordChanged` starts a Flow for each record created or changed in one
picked table. It polls with the `sdkgo/pollingtrigger` kit, since Airtable
offers no change feed to a personal access token without a webhook. The table
needs a field of type **Last modified time** with **Include time** turned on.
That field's own setting decides what counts as a change: all fields, or only
the fields it watches, so a field that watches only `Status` makes the Trigger
fire on status changes alone. Records whose watched field is empty, such as a
new record when the field watches fields the record never set, are not
reported until the field gets a value.

| Configuration | Unit | Stored value |
| --- | --- | --- |
| `baseId`, `baseName` | `basePicker` | Base ID, such as `app` followed by 14 letters and digits. |
| `tableId`, `tableName` | `tablePicker` | Table ID, which survives renames. |
| `fieldId`, `fieldName` | `lastModifiedTimeFieldPicker` | Last modified time field ID, which survives renames. |

Each poll reads the base schema once to find the watched field's current
name, then sends `POST /v0/{baseId}/{tableId}/listRecords` with
`filterByFormula` `IS_AFTER({fieldId}, DATETIME_PARSE("<latest delivered
change minus six seconds>"))`, `sort` ascending by the field ID, and
`pollPageSize` records.
Each `RecordChangedEvent` carries the base and table IDs, the parsed
`LastModifiedTime`, `IsCreatedRecord` (created and not edited since, within one
second), and the `Record` with fields keyed by name. The event ID is the
record ID and its modification time, such as
`recXXXXXXXXXXXXXX-20261115T090000123000000Z`, so a redelivered change maps to
the same Flow and a later edit of the same record starts a new one.

Cursor and delivery:

- The first poll seeds the cursor at the table's latest modification time
  without delivering existing records.
- The cursor stores the latest delivered modification time and the records
  delivered in the five seconds before it. Each poll lists from five seconds
  before that time, plus one second for formula rounding, and skips records it
  already delivered, so a change that becomes visible to the API up to five
  seconds after a later one is still delivered, and records that share a
  millisecond, which Airtable orders arbitrarily, are each delivered once.
- The cursor remembers at most 2,000 records. Past that, such as after a bulk
  edit, it forgets the oldest ones and stops looking before them once the scan
  has listed every record at their time. A scan longer than 20 pages of
  delivered records stores its Airtable `offset` in the cursor and resumes from
  it; further records come from immediate follow-up requests, up to ten pages
  per poll.
- The project runner records each change in the binding's durable inbox
  before the cursor commits past it, so a restart neither misses nor replays a
  change. Delivery is at least once, and Dex starts one Flow per event ID.
- The cursor is a time, so Airtable never expires it. An expired page offset
  (`422 LIST_RECORDS_ITERATOR_NOT_AVAILABLE`) starts the scan again from the
  cursor's time, still skipping the records it remembers. A stored cursor that
  cannot be decoded, or that belongs to a different field than the binding now
  names, is reseeded and logged as `poll cursor invalid; reseeding`; changes in
  that gap are not delivered.
- A renamed field is found again from the schema on the next poll. A field
  that is no longer a Last modified time field with a time of day, a missing
  table, a rejected token, or a missing scope fails the poll, which backs off
  up to five minutes and keeps the cursor.
- A `429`, or a per-base request queue more than two seconds long, pauses
  every binding of the connection for at least Airtable's 30 seconds. Polls
  share the client's 5-requests-per-second pacing per base with Steps in the
  same process; Airtable also allows 50 requests per second per token.

The poll reads only what the field records, so it cannot report deleted
records, and an edit that the watched field ignores is not a change. A change
that becomes visible to the API more than five seconds after a later change
was already delivered is not reported; Airtable documents no such delay.

The Worker builds a durable runner from the project configuration, as the
[`record-changed-intake`](go/examples/record-changed-intake) example does:

```go
triggerRunner, err := airtable.NewProjectRecordChangedTriggerRunner(project, recordchangedintake.ConnectionName, []airtable.ProjectRecordChangedTriggerRoute{{
	BindingName: recordchangedintake.RecordChangedTriggerBinding, Target: newChangedRecordTarget(client, flow, logger),
}}, airtable.WithLogger(logger))
```

`NewRecordChangedTriggerRunner` takes explicit routes, cursors, and targets
instead, and the generated `NewRecordChangedTrigger` keeps its cursor in
memory, so a restart seeds again. Log records carry the connector, connection,
Trigger, and binding, never field values, field names, or the token.

The Python package implements `recordChanged` at parity on the Python poll
Trigger kit: its cursor documents and event IDs match Go's byte for byte, so Go
and Python replicas can share one binding's cursor and inbox. See the
[Python README](python/README.md#recordchanged-trigger) and its
[`record_changed_intake`](python/examples/record_changed_intake/README.md)
example.

The TypeScript package implements `recordChanged` at parity on the TypeScript
poll Trigger kit: its cursor documents and event IDs match Go's byte for byte,
so Go and TypeScript replicas can share one binding's cursor and inbox. See the
[TypeScript README](typescript/README.md#recordchanged-trigger) and its
[`record-changed-intake`](typescript/examples/record-changed-intake/README.md)
example.

A push Trigger built on Airtable webhooks is not shipped. A webhook sends only
a ping, after which the receiver lists payloads from a stored cursor; it also
needs the `webhook:manage` scope, a public HTTPS endpoint, a MAC secret shown
once at creation, and a refresh every seven days.

## Example

[`examples/refund-decision`](go/examples/refund-decision) runs one Flow from Dex
Web **Start Flow** that uses every operation: it reads the refund policy row
whose key matches the request, decides the refund, upserts the case's decision
log row with a link to the policy, stamps the policy with the case, and reads
the log row back.

The Worker loads the project configuration once with
`projectconfig.LoadFromEnvironment` and opens the connection from it:

```go
connection, err := airtable.NewProjectConnection(project, refunddecision.ConnectionName)
if err != nil {
	return err
}
```

The policy lookup lists two rows, so a duplicate policy key is visible
instead of silently choosing one:

```go
// MapToFindRefundPolicyInput lists up to two policies with the request's key, so a duplicate key is visible.
func (flow *Flow) MapToFindRefundPolicyInput(decision RefundDecision) airtable.ListRecordsInput {
	return airtable.ListRecordsInput{
		BaseID: flow.settings.PolicyTable.BaseID, TableIDOrName: flow.settings.PolicyTable.TableID,
		Filters:  []airtable.FieldEqualityFilter{airtable.TextFieldEquals(PolicyKeyField, decision.Request.PolicyKey)},
		Fields:   []string{PolicyKeyField, ApprovalLimitField},
		PageSize: policyPageSizeThatRevealsAmbiguity,
	}
}
```

A typed cell is read back with its reader:

```go
limit, hasLimit := policies[0].Fields[ApprovalLimitField].Number()
if !hasLimit {
	decision.Decision, decision.Reason = DecisionNeedsReview, "the policy has no Approval Limit USD"
	return decision
}
```

The decision log row is merged on its case ID and links the policy record:

```go
// MapToUpsertDecisionLogInput writes the case's log row, merged on Case ID, linking the matched policy or none.
func (flow *Flow) MapToUpsertDecisionLogInput(decision RefundDecision) airtable.UpsertRecordsInput {
	policyLinks := airtable.LinkedRecordsCellValue()
	if decision.PolicyRecordID != "" {
		policyLinks = airtable.LinkedRecordsCellValue(decision.PolicyRecordID)
	}
	return airtable.UpsertRecordsInput{
		BaseID: flow.settings.LogTable.BaseID, TableIDOrName: flow.settings.LogTable.TableID,
		FieldsToMergeOn: []string{CaseIDField},
		Records: []airtable.RecordUpsert{{Fields: map[string]airtable.CellValue{
			CaseIDField:     airtable.TextCellValue(decision.Request.CaseID),
			CustomerField:   airtable.TextCellValue(decision.Request.Customer),
			AmountField:     airtable.NumberCellValue(decision.Request.AmountUSD),
			DecisionField:   airtable.TextCellValue(string(decision.Decision)),
			PolicyLinkField: policyLinks,
		}}},
	}
}
```

The example README lists the table fields, the Dex Web setup, and the Start
Flow input.

[`examples/record-changed-intake`](go/examples/record-changed-intake) starts
one Flow per record created or changed in a picked table through the
`recordChanged` Trigger and records the change for review in Dex Web.

## Verification

```bash
GOWORK=off go test -race ./...
GOWORK=off go vet ./...
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

The real-Dex example tests need a Dex development server:

```bash
dexcli dev
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/refund-decision/... -count=1 -v
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/record-changed-intake/... -count=1 -v
```

They cover an approved case end to end with a duplicate start, a rerun of one
case that updates its log row, a duplicate policy key logged for review, a
Worker replacement that sends one upsert and two converging absolute updates,
a `429` retried after Airtable's 30-second cooldown, and a rejected
formula that fails the Flow without provider text. The
`record-changed-intake` test serves a fake table and checks that existing
records start no Flow, each created or edited record starts exactly one
completed Flow, a renamed field keeps working, and a change made while the
runner is stopped arrives after a restart that first meets a `503`.

No live Airtable base was used, so the `recordChanged` formula,
`DATETIME_PARSE` of an ISO 8601 time, sorting by a field ID, and the
millisecond precision of Last modified time values are unverified against
Airtable. The operations, picker commands, error types,
and limits follow Airtable's published Web API reference; the 10-record write
limit is enforced as Airtable has documented it, although the current
reference pages no longer state it.
