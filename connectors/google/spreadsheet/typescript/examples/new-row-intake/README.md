# Google Sheets new-row intake Flow for TypeScript

This runnable example starts the `GoogleSheetsNewRowIntake` Flow once per row
appended to a picked Google Sheets tab:

1. the `rowAdded` Trigger polls the `new-intake-row` binding's spreadsheet and tab;
2. each new non-empty row is recorded in the binding's durable project inbox;
3. a Flow-start target starts the Flow `google-sheets-row-<event ID>`;
4. `RecordAddedRow` persists the header-keyed row;
5. `ReadAddedRow` reads that row back through the connection's `getValues`
   Step, as one whole-row range such as `'Leads'!7:7`;
6. `CompleteRowIntake` stores the cells read back beside the reported row and
   completes the Flow.

Rows already on the tab when the Worker first polls never start Flows. Row 1
must hold the column headers; each row's `values` maps those headers to the
row's formatted cells.

The cells read back differ from the reported row when the row was edited, or
rows above it moved, between the poll and the Flow run.

## Configure

Follow the [Google Sheets Connector setup](../../../README.md), then from
this directory generate the Flow's strict FDG 2.0 reference, which carries the
static binding with its `spreadsheetPicker` and `sheetTabPicker` units, and
start `dexcli dev` with it. The analyzer resolves
`@superdurable/connector-google-sheets` from `node_modules`, so install the
published package (or `npm pack` this package and install the tarball) first:

```sh
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ./build/new-row-intake.json --open=false
dexcli dev -flow-rendering-dir ./build
```

In Dex Web **Connections**, configure `google-sheets / google-sheets-intake`
and its `new-intake-row` binding of `rowAdded`:

- **Spreadsheet** opens the Google Picker and stores the spreadsheet ID.
- **Tab** loads the chosen spreadsheet's tab titles and stores the exact title.

The connection's `pollInterval` and `pollPageSize` settings apply to every
binding of the connection. Each poll normally makes one Sheets read request;
seeding and realigning after rows move reread the tab from row 1. Google allows
60 read requests per minute per user per project.

## Run

Set `DEX_CONNECTOR_CONFIG_FILE` to the owner-only `connections.json` that Dex
Web wrote, then from `connectors/google/spreadsheet/typescript` run:

```sh
npm ci
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:new-row-intake
```

[`app.ts`](app.ts) wires the stored binding to the Flow and runs it beside the
Dex Worker until SIGINT or SIGTERM. Dex Web projects keep poll cursors in
process memory, as Go does, so a local restart seeds again; hosted projects
keep them in project storage beside the binding's inbox, so a restarted replica
neither misses nor replays a row. Set `LOG_LEVEL=debug` to also see each cursor
commit and delivery. Records carry the binding identity and row numbers, never
cell values, tab titles, or tokens.

## Test

```sh
npm test
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

`test/new_row_intake_example.test.ts` checks the binding's visible fields,
guidance, and mappings. The real-Dex test in
[`new-row-intake.integration.test.ts`](../../test/integration/new-row-intake.integration.test.ts)
serves a loopback fake Sheets tab and checks that existing rows start no Flow,
each appended row starts exactly one completed Flow that reads the row back, a
deleted row above the cursor realigns it, and a row added while the runner is
stopped arrives after a restart that first answers 429. Polling a live Google spreadsheet has not
been verified.
