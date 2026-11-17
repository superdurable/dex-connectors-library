# Google Sheets Connector

Install the Go module with `go get github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet/go@v0.26.1`.

Moved from `github.com/superdurable/dex-connectors-library/connectors/google/spreadsheet`.

The Google Sheets Connector exposes operation-specific Dex Step factories:

- `spreadsheet.NewGetValuesStep`
- `spreadsheet.NewFindRowStep`
- `spreadsheet.NewUpsertRowStep`

| Name | Kind | What it does | Happy branch | Languages |
| --- | --- | --- | --- | --- |
| `getValues` | Query | Reads one bounded A1 range as formatted values | `read` | Go, Python, TypeScript |
| `findRow` | Query | Finds the one row whose key column holds a stable key | `found` | Go, Python, TypeScript |
| `upsertRow` | Mutation | Replaces the row with a stable key, or appends it when absent | `upserted` | Go, Python, TypeScript |
| `rowAdded` | Trigger | Each row appended below the last processed row of one picked tab | | Go, Python, TypeScript |

It uses a separate `google-sheets-oauth` Connection with the least-privilege
`drive.file` scope. A Google Picker grant selects each accessible spreadsheet;
manual IDs work only for files already granted to the application. The
generated `Credentials` keeps the OAuth client, access token, and refresh token
in the project connection that `NewProjectConnection` opens, as
[`examples/upsert-contact/main.go`](go/examples/upsert-contact/main.go) does. That
connection admits one refresh per credential generation across application
replicas and stores the complete replacement before the call uses it. Google
refresh-token rotation replaces the previous token only when Google returns a
new one, and terminal `invalid_grant` responses require reauthorization.

`UpsertRow` first reads the target sheet and matches a stable key column. It
[replaces values at an absolute range](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/update)
when one row matches, [appends a new row](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append)
when absent, and returns `conflict` for duplicate keys. A PUT can retry because
it replaces the same values. An append uses the shared single-dispatch fence
with synchronous Execute durability; the fence records its dispatch claim before
the POST and counts every possible send. A 429, 503, 401 followed by a
successful credential refresh, or traced failure before a connection is
obtained permits a retry. An unknown append outcome, such as a timeout, a lost
connection, a 5xx, a 409, or an unreadable response, is read back by the row
key; a visible row with the requested values confirms the append. Otherwise the
append is resent on a later attempt, which reads the key again first, until the
send cap (`maxSends`, default 3) is reached. Only an append still unconfirmed at
the cap returns `uncertain`. A definite refusal (400, 401, 403, 404, 411, 413,
414, 415, or 422) returns `providerRejected` on the first send and is never
resent; after an earlier unconfirmed append it returns `uncertain`.

Google [recommends backoff for 503](https://developers.google.com/workspace/sheets/api/troubleshoot-api-errors),
but its published [append documentation](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/append)
does not guarantee that 503 means the row was not applied, or that a
row-key lookup immediately sees every accepted append. The connector treats
503 as not-processed evidence; that provider behavior and read
visibility timing are unverified against a live Google spreadsheet. The
connector's `X-Idempotency-Key` is a stable correlation header; Google does not
document deduplication for it. A delayed row becoming visible after a retry's
lookup could still lead to duplicate rows.

Every operation uses `defect` for invalid local input, connection configuration,
or Connector contract violations. `GetValues` and `FindRow` also select `defect`
for any credential resolution failure, including transient refresh failures.
Before an upsert write, terminal reauthorization failures such as `invalid_grant`
select `defect` only after a not-sent checkpoint; transient credential failures
retry after that checkpoint. If an earlier append may have been accepted, a
terminal credential failure, a rejected or invalid lookup, or a local defect
selects `uncertain`, while a transient failure retries without sending again. A
conclusive Google API refusal of a first send uses `providerRejected`; malformed
or oversized reads before any write use `invalidResponse`. An unreadable
dispatch checkpoint retries without writing. Safe query transport, rate-limit, and availability failures
retry instead of producing a branch.

## rowAdded Trigger

`rowAdded` delivers each row appended below the last processed row of one
picked spreadsheet tab. The Sheets API has no change notifications for cell
values ([API concepts](https://developers.google.com/workspace/sheets/api/guides/concepts)),
so the Trigger polls with the shared `sdkgo/pollingtrigger` kit. Each binding
stores `spreadsheetId` and `sheetName` from the `spreadsheetPicker` and
`sheetTabPicker` units; row 1 of the tab is the header row. Applications run
every binding of a connection through one durable project runner, as
[`examples/new-row-intake/main.go`](go/examples/new-row-intake/main.go) does:

```go
triggerRunner, err := spreadsheet.NewProjectRowAddedTriggerRunner(project, newrowintake.ConnectionName, []spreadsheet.ProjectRowAddedTriggerRoute{{
	BindingName: newrowintake.NewRowTriggerBinding, Target: newAddedRowTarget(client, flow, logger),
}}, spreadsheet.WithLogger(logger))
```

- **Seeding.** The first poll stores the tab's current last row without
  delivering existing rows. It reads the tab from row 1 in `maxRows` windows.
- **Polling.** Each later poll makes one
  [`values:batchGet`](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values/batchGet)
  request for the header row and up to `pollPageSize` rows after the cursor,
  using whole-row A1 ranges such as `'Leads'!12:112`. Sheets omits trailing
  empty rows and columns from each
  [`ValueRange`](https://developers.google.com/workspace/sheets/api/reference/rest/v4/spreadsheets.values),
  so a short page means the tab ends there. Empty rows are skipped. A full page
  is followed at once by the next one.
- **Event.** `RowAddedEvent` carries the spreadsheet ID, tab title, row number,
  header-keyed `values`, and ordered `cells`, all formatted values. Its ID is
  `row-<row number>-<hash>`, a pure function of the tab, row number, and cells,
  so a refetch after a crash repeats the same ID.
- **Cursor.** The durable cursor holds the last processed row number and a
  SHA-256 of its cells, stored beside the binding's Trigger inbox. It commits
  only after the inbox recorded every delivered row. When the cursor row moved
  because rows above it were deleted or inserted, the runner finds the row with
  the same cells nearest the old number and continues after it. When no row
  matches, it treats the row as edited in place and keeps its number. When the
  tab now ends above it, it reseeds, so rows added in that gap are lost and the
  runner logs the reset. Rows inserted above the cursor are not reported as
  added. A realignment rereads the tab from row 1, so it costs one request per
  `maxRows` rows; a scan window whose response exceeds `maxResponseBytes` is
  halved, down to 100 rows, and read again.
- **Limits.** The cursor tells rows apart only by position and cells, so
  some changes are ambiguous. Deleting the last processed row while a new row
  arrives in the same poll interval looks like an edit of that row, so the new
  row is not delivered. Sorting or moving rows so that the last processed row
  lands higher delivers the rows below its new position again, with new event
  IDs. A run of at least `pollPageSize` blank rows directly below the last
  processed row hides rows typed after it until the gap is filled or
  `pollPageSize` grows. The tab title is used exactly as picked, so renaming
  the tab makes every poll fail with HTTP 400 until the binding is picked
  again.
- **Quota.** Google allows 60 read requests per minute per user per project and
  answers 429 when a quota is exceeded
  ([usage limits](https://developers.google.com/workspace/sheets/api/limits)).
  A 429 pauses every binding of the connection for at least its `Retry-After`,
  and other failures back off exponentially up to five minutes. Set
  `pollInterval` so that the bindings of one Google account stay under the
  quota.
- **Scope.** `values:batchGet` accepts the existing `drive.file` scope, so the
  Trigger needs no new consent.

The generated `NewRowAddedTrigger` factory keeps its cursor in memory: a
restart seeds again, and rows added while the process was stopped are not
delivered. Use it only in tests or applications without project storage.

The Trigger is verified against a fake Sheets server through a real Dex
Worker; polling a live Google spreadsheet, including its `values:batchGet`
responses for whole-row ranges, has not been verified. Only the Go module
implements `rowAdded`; the Python and TypeScript packages do not.

The `ui/` package builds the credential-safe Studio setup bundle published as
`connector-ui.tgz` with the Connector release.

## Python package

Install `dex-connector-google-spreadsheet==0.2.1` to use the Python `getValues`,
`findRow`, and `upsertRow` Step factories and the `rowAdded` Trigger. The package uses the manifest-generated
OAuth refresh driver and project connection. Its
[`upsert_contact` example](python/examples/upsert_contact/README.md) binds the
static `google-sheets-contacts` connection and verifies retry, deduplication
checkpointing, and optional-branch failure through a local Dex Worker and Client.
Its [`new_row_intake` example](python/examples/new_row_intake/README.md) starts
one Flow per added row through `new_project_row_added_trigger_runner`, with the
same cursor, realignment, and quota behavior as Go.
Go-recorded provider responses are replayed by the Python conformance suite.
Live Google authorization, scope consent, and spreadsheet read/write and poll
behavior have not been verified for this Python port.

## Test with spreadsheetmock

The generated `spreadsheetmock` package answers every operation's happy path by
default, and the defaults describe one sheet: a `Contacts` tab with the header
`email`, `name`, `status`, Ada Lovelace (`ada@example.test`, active) on row 2
and Grace Hopper (`grace@example.test`, pending) on row 3. `getValues` reads
that range and `findRow` finds Ada on row 2, whatever range or key the request
names. `upsertRow` answers `updated` on row 2 (`Contacts!A2:C2`) for any input,
so a Flow that expects an `inserted` result, another row, or another sheet
needs a script. Pass the typed connection to the example Flow to test without
Google credentials or response scripts:

```go
mock := spreadsheetmock.New(t, ConnectionName)
flow := NewFlow(mock.Connection())
```

This snippet comes from
[`workflow_mock_integration_test.go`](go/examples/upsert-contact/flow/workflow_mock_integration_test.go).
Named cases cover missing ranges or rows, duplicate keys, and provider refusals.

## Examples

[`examples/upsert-contact`](go/examples/upsert-contact) is a runnable Dex Web
**Start Flow** example that upserts one contact row by email address.
[`examples/new-row-intake`](go/examples/new-row-intake) starts one Flow per row
appended to a picked tab through the `rowAdded` Trigger and reads the row back
with a `getValues` Step.

## Verification

```bash
GOWORK=off go test -race -count=1 ./...
GOWORK=off go vet ./...
GOWORK=off go vet -tags=integration ./...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -p 1 -tags=integration ./... -count=1
npm ci --prefix ui
npm test --prefix ui
npm run build --prefix ui
```

## TypeScript package

`typescript/` publishes `@superdurable/connector-google-sheets` v0.2.0 with
`getValues`, `findRow`, `upsertRow`, and the `rowAdded` Trigger. Its
[`new-row-intake` example](typescript/examples/new-row-intake/README.md) starts
one Flow per added row through `newProjectRowAddedTriggerRunner`, with the same
cursor, realignment, and quota behavior as Go and cursor documents shared byte
for byte. Its runnable
[`upsert-contact` example](typescript/examples/upsert-contact/README.md) uses the
static `google-sheets-contacts` connection and generated OAuth refresh wiring.
The TypeScript package replays Go conformance scenarios and runs its Flow against
a real local Dex Worker and Client; live Google authorization and API writes
remain unverified.
