# Google Sheets new-row intake Flow for Python

This runnable example starts the `GoogleSheetsNewRowIntakePython` Flow once per
row appended to a picked Google Sheets tab:

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
`connectors/google/spreadsheet/python` generate the Flow's strict FDG 2.0
reference, which carries the static binding, and start `dexcli dev` with it:

```sh
uv run dexcli visualize ./examples/new_row_intake/flow.py \
  --schema-version 2.0 --json --out ./build/new-row-intake
dexcli dev -flow-rendering-dir ./build
```

In Dex Web **Connections**, configure `google-sheets / google-sheets-intake`
and its `new-intake-row` binding of `rowAdded`. The binding stores the
spreadsheet ID (the non-secret text between `/d/` and `/edit` in the
spreadsheet's `https://docs.google.com/spreadsheets/d/` address, for a file
already granted to the app) and the exact tab title. The Flow declares
`spreadsheetPicker` and `sheetTabPicker` units for them in
`get_connector_trigger_bindings`, but Dex CLI v1.47.3 does not read Python
configuration UI objects, so Dex Web does not show those pickers yet; see the
[package README](../../README.md#rowadded-trigger).

The connection's `pollInterval` and `pollPageSize` settings apply to every
binding of the connection. Each poll normally makes one Sheets read request;
seeding and realigning after rows move reread the tab from row 1. Google allows
60 read requests per minute per user per project.

## Run

Set `DEX_CONNECTOR_CONFIG_FILE` to the owner-only `connections.json` that Dex
Web wrote, then run the Worker and the poll runner:

```sh
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.new_row_intake.main
```

[`intake.py`](intake.py) wires the stored binding to the Flow, and
[`main.py`](main.py) runs it beside the Dex Worker until SIGINT or SIGTERM.
Dex Web projects keep poll cursors in process memory, as Go does, so a local
restart seeds again; hosted projects keep them in project storage beside the
binding's inbox, so a restarted replica neither misses nor replays a row. Set
`LOG_LEVEL=debug` to also see each cursor commit. Records carry the binding
identity and row numbers, never cell values, tab titles, or tokens.

## Test

```sh
uv run pytest tests/test_new_row_intake_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
  uv run pytest examples/new_row_intake/integration_test.py
```

The real-Dex test in [`integration_test.py`](integration_test.py) serves the
in-process [`sheets_fake.py`](sheets_fake.py) tab and checks that existing rows
start no Flow, each appended row starts exactly one completed Flow that reads
the row back, a deleted
row above the cursor realigns it, and a row added while the runner is stopped
arrives after a restart that first answers 429. Polling a live Google
spreadsheet has not been verified.
