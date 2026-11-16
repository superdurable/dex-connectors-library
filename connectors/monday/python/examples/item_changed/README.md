# monday.com item-changed example (Python)

This example starts one `MondayItemChangePython` Flow for every active item
created or changed on the watched board, through the `itemChanged` Trigger:

1. `RecordItemChange` validates the delivered change and records it.
2. `ReadChangedItem` calls `new_get_item_step`, so the record holds the item's
   current state rather than the poll's copy.
3. `CompleteItemChange` records the item read back and completes the Flow.

The Flow ID is `monday-item-change-<Trigger event ID>`, and the Trigger event
ID is stable per item version, so a redelivered change finds its existing Flow
instead of starting a second one. Only the happy-path `found` branch is wired,
so an item archived before the Flow reads it fails the Flow. It binds the
static connection `monday-board-watcher` and the Trigger binding
`board-item-changes`.

`worker.py` is the Worker entrypoint. It loads the project, serves the Flow,
and runs `new_project_item_trigger_runner`, which records each change in the
binding's project Trigger inbox before the board position moves past it and
starts one Flow per Trigger event ID with `new_dex_flow_trigger_target`. The
first start records the board's existing items as a baseline and starts no Flow
for them. See the [package README](../../README.md#itemchanged-trigger) for
change kinds, batches, resets, and rate limits.

## Configure

1. Follow the [monday.com Connector setup](../../../README.md#mondaycom-setup),
   then configure the `monday-board-watcher` connection under **Connections**
   in Dex Web. Its **Poll interval** is the delay between polls, at least 30
   seconds; every poll costs at least one monday.com API call against the
   account's daily call limit.
2. Save the `board-item-changes` binding's watched board. Dex CLI v1.47.3 reads
   the Python binding but reports `connector_configuration_ui` for its
   `ConnectorConfigurationUI` literal, so Dex Web does not show the
   `boardIdInput` field for it yet. Enter the binding configuration
   `{"boardId": "<board ID>"}`, the digits after `/boards/` in the board's
   https://monday.com/ address, in the binding's `triggerBindings` entry of
   `connections.json`. The connection's user must be able to read the board;
   the Worker refuses to start without a saved board.

The Worker reads the saved values once at startup, so restart it after saving.
A changed board records a new baseline.

## Run

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or newer from
`connectors/monday`:

```bash
mkdir -p python/build
dexcli visualize ./python/examples/item_changed/flow.py \
  --schema-version 2.0 --json --out ./python/build/item_changed
```

Start a local Dex server, such as
`dexcli dev -dex-port 22240 -web-port 22241 -open=false -flow-rendering-dir python/build`,
then from `connectors/monday/python` run `uv sync --locked` and:

```bash
DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run python -m examples.item_changed.worker
```

Override `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed. With hosted project
storage a restarted Worker neither misses nor repeats a change. Under
`dexcli dev` the poll cursor stays in process memory, so a restart records a
new baseline and changes made while stopped are not delivered.

## Test

`tests/test_item_changed_example.py` checks the Flow wiring, mappers, and
binding; `tests/test_item_changed_fetcher.py`,
`tests/test_item_trigger_runner.py`, and `tests/test_item_changed_go_parity.py`
cover the Trigger. With `dexcli dev` running,

```bash
DEX_FLOW_SERVICE_ADDRESS=localhost:22240 uv run pytest integration/test_item_changed.py
```

runs the example's Flow and target against real Dex and a local stand-in for
monday.com's GraphQL API: the baseline starts no Flow, each change starts
exactly one Flow, changes made while the Trigger process is stopped arrive after
a restart whose first request is rate limited, and more than 10,000 changes
since the last poll record a new baseline without delivering the gap. Live
monday.com polling has not been verified.
