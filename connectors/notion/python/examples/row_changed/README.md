# Notion row-changed example

Every row created or edited in the watched Notion data source starts one
`NotionDatabaseRowChangePython` Flow through the `databaseRowChanged` poll
Trigger. The Flow records the delivered change, reads the row back with
`new_get_page_step` (properties only), and completes with both. It binds the
static Dex connection `notion-row-watcher` and the Trigger binding
`database-row-changes`; `getPage`'s optional branches are unwired, so a
provider rejection fails the Flow.

`main.py` is the Worker entrypoint. It loads the project, runs the Worker, and
runs `new_project_database_row_trigger_runner`, which records each row version
in the binding's project Trigger inbox before the cursor moves past it and
starts one Flow per Trigger event ID with `new_dex_flow_trigger_target`. The
first start records a baseline and starts no Flow for existing rows; a trashed
row is never delivered.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer from
`connectors/notion`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/row_changed/flow.py \
  --schema-version 2.0 --json --out ./python/build/row_changed
```

The output must contain `"valid": true`. Dex CLI v1.47.3 reports a
`connector_configuration_ui` warning for the Python binding's
`ConnectorConfigurationUI` literal, so Dex Web does not show the
`dataSourcePicker` unit for the Python binding yet.

1. In Dex Web Connections, configure a Notion internal connection API token for
   `notion-row-watcher`, following the connector's authorization guide, and
   share the watched database with the connection. Its `pollInterval` setting
   is the wait between polls, at least 5 seconds, and `pollPageSize` the rows
   per query page.
2. Save the `database-row-changes` binding's data source. Until Dex Web shows
   the picker for the Python binding, enter the binding configuration
   `{"dataSourceId": "<data source ID>"}` in the binding's `triggerBindings`
   entry of `connections.json`; copy the ID from the database's settings >
   Manage data sources > Copy data source ID at https://app.notion.com.
3. Start a local Dex server, such as
   `dexcli dev -dex-port 22240 -web-port 22241 -open=false -flow-rendering-dir python/build`.
4. From `connectors/notion/python`, run `uv sync --locked`, then
   `DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run python -m examples.row_changed.main`.
   Restart it after changing the binding; a changed data source records a new
   baseline. Under `dexcli dev` the poll cursor stays in process memory, so a
   restart also records a new baseline.

The deterministic tests are `tests/test_row_changed_example.py`,
`tests/test_database_row_trigger.py`, and
`tests/test_database_row_go_fixtures.py`, which replays polls recorded from the
Go fetcher and checks the same requests, cursor bytes, and Trigger event IDs.
With `dexcli dev` running,
`DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest examples/row_changed/integration_test.py`
runs the example's Flow and target against real Dex and a local stand-in for
the Notion data source query: the baseline starts no Flow, each row version
starts one Flow, a trashed row starts none, edits made while the Trigger
process is stopped arrive after a restart, a 429 pauses the poll for
Retry-After, and a stored cursor for another data source records a new
baseline without delivering existing rows. Live Notion authorization and data
source query behavior require a user-controlled connection and remain
unverified.
