# monday.com item-changed example

This example starts one `MondayItemChange` Flow for every active item created
or changed on the watched board, through the `itemChanged` Trigger:

1. `RecordItemChange` validates the delivered change and records it.
2. `ReadChangedItem` calls `monday.NewGetItemStep`, so the record holds the
   item's current state rather than the poll's copy.
3. `CompleteItemChange` records the item read back and completes the Flow.

The Flow ID is `monday-item-change-<Trigger event ID>`, and the Trigger event
ID is stable per item version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired, so an item archived before the Flow reads it fails the Flow.

The first start records the board's existing items as a baseline and starts no
Flow for them. See the
[connector README](../../../README.md#itemchanged-trigger) for change kinds,
batches, resets, and rate limits.

## Configure

Follow the [monday.com Connector setup](../../../README.md#mondaycom-setup),
then configure the `monday-board-watcher` connection under **Connections** in
Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 30
  seconds. Every poll costs at least one monday.com API call against the
  account's daily call limit.
- **Watched board** on the `board-item-changes` Trigger binding takes the
  board's numeric ID: open the board at https://monday.com/ and copy the digits
  after `/boards/` in its address, or paste the whole address. The
  connection's user must be able to read the board. The binding is required;
  the Worker refuses to start without a saved board.

The Worker reads the saved values once at startup, so restart it after saving.
A changed board records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/monday/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/item-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/item-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/item-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/item-changed/...
GOWORK=off go test -tags=integration ./examples/item-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for monday.com's GraphQL API. They check that the baseline starts no
Flow, that each change starts exactly one Flow, and that changes made while the
Trigger process is stopped arrive after a restart whose first request is rate
limited. They also check that more than 10,000 changes since the last poll
record a new baseline without delivering the gap. Live monday.com polling has
not been verified.
