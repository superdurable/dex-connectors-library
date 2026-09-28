# Google Sheets upsert-contact example

This example runs one operation-only Flow from Dex Web **Start Flow**:

1. `RecordContactUpsertRequest` validates and persists the typed contact;
2. `UpsertContactRow` calls `spreadsheet.NewUpsertRowStep` using email as the key;
3. `CompleteContactUpsert` persists the provider result and completes the Flow.

Generate strict FDG 2.0 from `connectors/google/spreadsheet`:

```bash
mkdir -p build
dexcli visualize ./examples/upsert-contact/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/upsert-contact
```

Run `dexcli dev` with that build directory, configure
`google-sheets / google-sheets-contacts` under **Connections**, then run the
Worker with the connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
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

```bash
GOWORK=off go test -race ./examples/upsert-contact/...
```
