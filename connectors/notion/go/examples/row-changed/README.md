# Notion row-changed example

This example starts one `NotionDatabaseRowChange` Flow for every row created
or edited in the picked data source, through the `databaseRowChanged`
Trigger:

1. `RecordRowChange` validates the delivered change and records it.
2. `ReadChangedRow` calls `notion.NewGetPageStep` without body blocks, so the
   record holds the row's current properties rather than the polled copy.
3. `CompleteRowChange` records the row read back and completes the Flow.

The Flow ID is `notion-row-change-<Trigger event ID>`, and the Trigger event
ID is stable per row version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired; a row trashed or unshared before the read fails the Flow
through the unwired `notFound` branch.

The first start records the data source's existing rows as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#databaserowchanged-trigger) for the
cursor, change kinds, limits, and rate limits.

## Configure

Follow the [Notion setup](../../../README.md#notion-setup), then configure the
`notion-row-watcher` connection under **Connections** in Dex Web. Share the
watched database with the connection.

- **Poll interval** on the connection is the wait between polls, at least 5
  seconds. Shorter intervals deliver changes sooner and spend more of the
  connection's Notion rate limit.
- **Poll page size** on the connection is the rows per query page, 1 to 100.
  Lower it only when wide rows exceed **Max response bytes**.
- **Watched database** on the `database-row-changes` Trigger binding takes a
  database link from the database's **•••** menu > **Copy link** and lists
  its data sources live from Notion. Pick one, or paste a data source ID
  from the database's settings > **Manage data sources** > **Copy data source
  ID** and choose **Check data source**. It saves the data source ID and
  name. The binding needs a data source, so the Worker exits with an error
  until one is saved.

The Worker reads the saved values once at startup, so restart it after saving.
A changed data source records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/notion/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/row-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/row-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/row-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Edits made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/row-changed/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/row-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for the Notion data source query. They check that the baseline
starts no Flow, that each row version starts exactly one Flow, that a trashed
row starts none, and that edits made while the Trigger process is stopped
arrive after a restart. They also check that a 429 pauses for `Retry-After`
and that a cursor for another data source records a new baseline. Live
Notion polling has not been verified.
