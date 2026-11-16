# Google Sheets connector for TypeScript

This package binds `getValues`, `findRow`, and `upsertRow` to Dex Steps. The
`google-sheets-contacts` example in `examples/upsert-contact/` is a runnable
Flow that reads a stable contact key and replaces or appends a row. The upsert
uses a durable single dispatch fence before an append and confirms uncertain
responses by reading the key again.

Use `newProjectConnection(project, ConnectionName)` in a Worker after loading
Dex project configuration. The generated OAuth refresh driver exchanges stored
Google refresh tokens, checks the `drive.file` scope, and persists refreshed
credentials through the host. Keep credentials in Dex Web Connections, never
in Flow input or persisted Attributes.

Run `npm run check` for the package suite and
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration` against
`dexcli dev`. The integration test starts a local Sheets stand-in on an OS
assigned port and exercises the example through a real Dex Worker and Client.
Live Google OAuth consent, spreadsheet selection, and API writes are unverified.

## rowAdded Trigger

Version 0.2.0, on `@superdurable/connectors-sdk` 0.12.0, adds the polled
`rowAdded` Trigger at behavior parity with the Go connector. It delivers each
non-empty row appended below the last processed row of one picked tab:

- The first poll seeds the cursor at the tab's current last row without
  delivering existing rows.
- Each later poll makes one `values:batchGet` request for the header row and up
  to `pollPageSize` rows after the cursor, such as `'Leads'!12:112`; a full
  page is followed at once by the next one, and blank rows are skipped.
- `RowAddedEvent` carries `spreadsheetId`, `sheetName`, `rowNumber`,
  header-keyed `values`, and ordered `cells`. Its event ID,
  `row-<row number>-<hash>`, is a pure function of the tab, row number, and
  cells.
- The cursor stores the last processed row number and a SHA-256 of its cells.
  A cursor row moved by deletes or inserts above it is found again, an edited
  cursor row keeps its number, and a tab that now ends above the cursor reseeds
  and logs the gap. The cursor token, row hashes, and event IDs match the Go
  connector byte for byte, so Go and TypeScript replicas share one cursor.
- HTTP 429 pauses every binding of the connection for at least its
  `Retry-After`; other failed polls back off up to five minutes.

`newProjectRowAddedTriggerRunner` opens the connection from the loaded project,
decodes each route's saved binding, and keeps a durable cursor beside each
binding's Trigger inbox, as
[`examples/new-row-intake/app.ts`](examples/new-row-intake/app.ts) does:

```ts
const runner = newProjectRowAddedTriggerRunner(project, ConnectionName, [{ bindingName: NewRowTriggerBinding, target: newAddedRowTarget(client, flow, options.logger) }], { ...options.spreadsheetOptions, triggerLogger: options.logger });
```

`newRowAddedTriggerRunner` takes explicit routes with cursors and targets, and
`newRowAddedTrigger` builds one binding's `TriggerRunner` with an in-memory
cursor, so a restart seeds again; use it only in tests or applications without
project storage. Records go to the client's `triggerLogger` and carry the
binding identity and row numbers, never cell values, tab titles, or tokens. The
Go connector's `WithLogger` option is `SpreadsheetClientOptions.triggerLogger`,
and `pollInterval` is `Config.pollIntervalMs`; `newProjectConnection` converts
the stored Go nanoseconds.

The integration test in
[`test/integration/new-row-intake.integration.test.ts`](test/integration/new-row-intake.integration.test.ts)
runs the example Flow on a real Dex Worker against a loopback fake Sheets tab.
Polling a live Google spreadsheet has not been verified.
