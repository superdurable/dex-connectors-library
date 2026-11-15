# Google Sheets new-row-intake example

This example starts one Flow per row appended to a picked Google Sheets tab:

1. the `rowAdded` Trigger polls the `new-intake-row` binding's spreadsheet and tab;
2. each new non-empty row is recorded in the binding's durable project inbox;
3. a Flow-start target starts `GoogleSheetsNewRowIntake` with the Flow ID
   `google-sheets-row-<event ID>`;
4. `RecordAddedRow` persists the header-keyed row;
5. `ReadAddedRow`, a `getValues` Step on the same connection, reads that row
   number back as a whole-row range such as `'Leads'!7:7`;
6. `CompleteRowIntake` records the row's current cells and completes the Flow.

Rows already on the tab when the Worker first polls never start Flows. Row 1
must hold the column headers; each row's `values` maps those headers to the
row's formatted cells. The Flow output keeps the reported row and the cells
read back; they differ when the row was edited, or rows above it moved, after
the poll. A row that is now empty reads back as no cells.

## Configure

Follow the [Google Sheets Connector setup](../../../README.md), then in Dex Web
**Connections** configure `google-sheets / google-sheets-intake` and its
`new-intake-row` binding:

- **Spreadsheet** opens the Google Picker and stores the spreadsheet ID.
- **Tab** loads the chosen spreadsheet's tab titles and stores the exact title.

The connection's `pollInterval` and `pollPageSize` settings apply to every
binding of the connection. Each poll normally makes one Sheets read request;
seeding and realigning after rows move reread the tab from row 1. Google allows
60 read requests per minute per user per project. The connector README lists
the edits the cursor cannot tell apart, such as sorting the tab.

Generate strict FDG 2.0 from `connectors/google/spreadsheet/go`:

```bash
mkdir -p build
dexcli visualize ./examples/new-row-intake/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/new-row-intake
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/new-row-intake
```

## Delivery and logs

The runner commits the binding's cursor, the last processed row number and a
SHA-256 of its cells, only after the inbox recorded every row before it, so a
restart or a second replica neither misses nor replays a row. When rows above
the cursor are deleted or inserted, the runner finds the cursor row again and
logs `sheet rows moved; cursor realigned`. When the cursor row itself was
edited, it keeps the row number. When the tab now ends above the cursor, it
reseeds and logs `poll cursor invalid; reseeding`; rows added in that gap are
not delivered. Rows inserted above the cursor are never reported as added.

HTTP 429 pauses every binding of the connection for at least its
`Retry-After`; other failed polls back off up to five minutes. Records carry
the connector, connection, Trigger, binding, and row numbers, never cell values,
tab titles, or tokens. Set `LOG_LEVEL=debug` to also see each cursor commit and
delivery.

## Test

```bash
GOWORK=off go test -race ./examples/new-row-intake/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/new-row-intake/...
```

The real-Dex test in
[`row_added_integration_test.go`](flow/row_added_integration_test.go) serves
a fake Sheets tab and checks that existing rows start no Flow, each appended
row starts exactly one completed Flow that reads the row back, a deleted row above the cursor realigns
it, and rows added while the runner is stopped arrive after a restart that
first answers 429.
