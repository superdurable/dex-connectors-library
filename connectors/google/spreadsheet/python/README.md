# Google Sheets Python Connector

This package exposes `getValues`, `findRow`, and `upsertRow` as Dex Python Steps and the polled `rowAdded` Trigger. Install `dex-connector-google-spreadsheet==0.2.0`, which requires `dex-connectors-sdk>=0.12.0`, and create a `google-sheets-oauth` project connection. See [the connector README](../README.md) for Google Cloud OAuth setup, the append safety model, and the `rowAdded` cursor rules.

The `examples/upsert_contact` application reads or upserts a contact in a selected spreadsheet. Run its integration test with a local Dex development server; live Google authorization and provider behavior have not been verified for this port.

## rowAdded Trigger

`rowAdded` delivers each non-empty row appended below the last processed row of one picked tab, at behavior parity with the Go connector. The Sheets API has no change notifications for cell values, so each binding polls with the Python Connector SDK's `pollingtrigger` kit:

- The first poll seeds the cursor at the tab's current last row without delivering existing rows.
- Each later poll makes one `values:batchGet` request for the header row and up to `pollPageSize` rows after the cursor, such as `'Leads'!12:112`, and a full page is followed at once by the next one. Blank rows are skipped.
- `RowAddedEvent` carries `spreadsheet_id`, `sheet_name`, `row_number`, header-keyed `values`, and ordered `cells`. Its event ID, `row-<row number>-<hash>`, is a pure function of the tab, row number, and cells.
- The cursor stores the last processed row number and a SHA-256 of its cells. A cursor row moved by deletes or inserts above it is found again; an edited cursor row keeps its number; a tab that now ends above the cursor reseeds and logs the gap. The cursor token, row hashes, and event IDs match the Go connector byte for byte, so Go and Python replicas share one cursor and inbox.
- HTTP 429 pauses every binding of the connection for at least its `Retry-After`; other failed polls back off exponentially up to five minutes.

`new_project_row_added_trigger_runner` opens the connection from the loaded project, decodes each route's saved binding, and keeps a durable cursor beside each binding's Trigger inbox, as [`examples/new_row_intake/intake.py`](examples/new_row_intake/intake.py) does:

```python
return await new_project_row_added_trigger_runner(
    project,
    CONNECTION_NAME,
    [ProjectRowAddedTriggerRoute(NEW_ROW_TRIGGER_BINDING, new_added_row_target(dex_client, flow))],
    logger=logger,
)
```

`new_row_added_trigger_runner` takes explicit `RowAddedTriggerRoute` cursors and targets, and `new_row_added_trigger` builds one binding's `TriggerRunner` with an in-memory cursor, so a restart seeds again; use it only in tests or applications without project storage. Records go to the `dex_connectors.sdk.trigger` logger, or the `logger` you pass, and carry the binding identity and row numbers, never cell values, tab titles, or tokens.

Differences from Go: the project runner is async and owns its connection, so call `aclose` after `run` returns; durations are seconds (`Config.poll_interval_seconds`); and the Go `WithLogger` option is the runners' `logger` argument.

Dex CLI v1.47.3 reads a Python Trigger binding's `configuration_ui` only from a dict literal, which the Python SDK's `TriggerBindingDefinition` does not accept. The example therefore declares its `spreadsheetPicker` and `sheetTabPicker` units as SDK objects, and `dexcli visualize` reports `connector_configuration_ui` and records no units for the binding, so Dex Web does not show those pickers for the Python Flow yet.

## Verification

```sh
uv sync --locked
uv run ruff check . && uv run mypy --strict src && uv run pytest
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/new_row_intake/integration_test.py
```

The integration test runs the example Flow on a real Dex Worker against an in-process fake Sheets tab. Polling a live Google spreadsheet has not been verified.
