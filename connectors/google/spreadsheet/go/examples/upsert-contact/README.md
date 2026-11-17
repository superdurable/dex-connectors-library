# Google Sheets upsert-contact example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordContactUpsertRequest` validates and persists the typed contact;
2. `UpsertContactRow` calls `spreadsheet.NewUpsertRowStep` using email as the key;
3. `CompleteContactUpsert` persists the provider result and completes the Flow.

It also registers `GoogleSheetsSpreadsheetDetail`, which reads a spreadsheet's title and tabs, and `GoogleSheetsOperations`, which routes one request to `createSheet`, `deleteSheet`, `clearValues`, `appendRow`, or `updateRow`. The mock-backed integration test runs all seven paths through a real Dex Worker and Client.

Generate strict FDG 2.0 from `connectors/google/spreadsheet`:

```bash
mkdir -p build
dexcli visualize ./examples/upsert-contact/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/upsert-contact
dexcli visualize ./examples/upsert-contact/flow/spreadsheet_operations.go \
  --schema-version 2.0 --json --out ./build/spreadsheet-operations
```

Run `dexcli dev` with that build directory, configure
`google-sheets / google-sheets-contacts` under **Connections**, then run the
Worker. It reads the `DEX_PROJECT_*` project configuration environment
documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading);
Dex Web or Superverse Studio writes that configuration:

```bash
go run ./examples/upsert-contact
```

Start `GoogleSheetsUpsertContact` with a unique Flow ID:

```json
{
  "spreadsheetId": "your-spreadsheet-id",
  "sheetName": "Contacts",
  "email": "person@example.com",
  "name": "Person",
  "status": "active"
}
```

The spreadsheet must contain an `email` header. Only the `upserted` branch is
wired; conflicts, provider rejection, invalid responses, uncertain writes, and
local defects fail the Flow.

Start `GoogleSheetsOperations` with an `operation` and its fields. For example,
`{"operation":"appendRow","spreadsheetId":"your-spreadsheet-id","sheetName":"Contacts","values":["person@example.com","Person","active"]}`
appends a row. Use `sheetName` for `createSheet`, `sheetId` for `deleteSheet`,
`range` for `clearValues`, and `rowNumber` plus `values` for `updateRow`.
These writes change the selected spreadsheet; `deleteSheet` removes its tab.

The real-Dex retry test in
[`workflow_retry_integration_test.go`](flow/workflow_retry_integration_test.go)
serves one 503 response followed by success and checks that the Flow completes
after exactly two append requests with the same correlation key. CI runs this
test; local verification does not start a Dex stack.

```bash
GOWORK=off go test -race ./examples/upsert-contact/...
```
