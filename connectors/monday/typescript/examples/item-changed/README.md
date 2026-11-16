# monday.com item-changed example (TypeScript)

This example starts one `MondayItemChangeTypeScript` Flow for every active item
created or changed on the watched board, through the `itemChanged` Trigger:

1. `RecordItemChange` validates the delivered change and records it.
2. `ReadChangedItem` calls `newGetItemStep`, so the record holds the item's
   current state rather than the poll's copy.
3. `CompleteItemChange` records the item read back and completes the Flow.

The Flow ID is `monday-item-change-<Trigger event ID>`, and the Trigger event
ID is stable per item version, so a redelivered change finds its existing Flow
instead of starting a second one. Only the happy-path `found` branch is wired,
so an item archived before the Flow reads it fails the Flow. It binds the
static connection `monday-board-watcher` and the Trigger binding
`board-item-changes`.

`main.ts` is the Worker entrypoint. `runItemChangeWorker` in `app.ts` serves
the Flow and runs `newProjectItemTriggerRunner`, which records each change in
the binding's project Trigger inbox before the board position moves past it and
starts one Flow per Trigger event ID with `newDexFlowTriggerTarget`. The first
start records the board's existing items as a baseline and starts no Flow for
them. See the [package README](../../README.md#itemchanged-trigger) for change
kinds, batches, resets, and rate limits.

## Configure

Follow the [monday.com Connector setup](../../../README.md#mondaycom-setup),
then configure the `monday-board-watcher` connection under **Connections** in
Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 30
  seconds. Every poll costs at least one monday.com API call against the
  account's daily call limit.
- **Watched board** on the `board-item-changes` Trigger binding takes the
  board's numeric ID: open the board at https://monday.com/ and copy the digits
  after `/boards/` in its address, or paste the whole address. The connection's
  user must be able to read the board. The binding is required; the Worker
  refuses to start without a saved board.

The Worker reads the saved values once at startup, so restart it after saving.
A changed board records a new baseline.

## Run

From `connectors/monday/typescript`, build the package, make it resolvable from
the example, and generate strict FDG 2.0:

```bash
npm ci && npm run build
npm install --prefix examples/item-changed --no-save --no-package-lock --ignore-scripts .
cd examples/item-changed
dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/item-changed --open=false
```

Run `dexcli dev -flow-rendering-dir ./build`, configure the connection and the
binding's board in Dex Web, set `DEX_CONNECTOR_CONFIG_FILE` to the
`connections.json` Dex Web wrote, and start the Worker, which also runs the
Trigger:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:item-changed
```

Set `LOG_LEVEL` to `debug`, `info`, `warn`, or `error`; Trigger records carry
no payloads or credentials.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
as in Go, so a restarted local Worker records a new baseline and changes made
while it was stopped are not delivered.

## Test

```bash
npx vitest run test/item_changed_workflow.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration/item-changed.integration.test.ts
```

The integration tests run `runItemChangeWorker` on a real Dex Worker and Client
against a local stand-in for monday.com's GraphQL API, with project storage
shared by two processes. They check that the baseline starts no Flow, that each
change starts exactly one Flow, and that changes made while the process is
stopped arrive after a restart whose first request is rate limited. They also
check that more than 10,000 changes since the last poll record a new baseline
without delivering the gap. Live monday.com polling has not been verified.
