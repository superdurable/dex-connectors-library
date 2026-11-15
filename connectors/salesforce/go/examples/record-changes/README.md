# Salesforce record-changes example

This example starts one `SalesforceRecordChange` Flow for every record of the
picked object that is created or modified, through the `recordModifiedPolled`
Trigger:

1. `RecordRecordChange` validates the delivered change and records it.
2. `ReadChangedRecord` calls `salesforce.NewGetRecordStep` for every field the
   change carries, so the outcome holds the record's current values rather
   than the polled copy.
3. `CompleteRecordChange` records the record read back and completes the Flow.
4. `CompleteDeletedRecord` handles the `notFound` branch: a record deleted
   after the poll read it completes as deleted instead of failing the Flow.

The Flow ID is `salesforce-record-change-<Trigger event ID>`, and the Trigger
event ID is stable per record and `SystemModstamp`. A redelivered change
therefore finds its existing Flow instead of starting a second one. Other
`getRecord` branches are unwired and fail the Flow.

The first start records the object's existing records as a baseline and starts
no Flow for them. See the
[connector README](../../../README.md#polling-trigger-recordmodifiedpolled) for
change kinds, the settle delay, deletions, and API limits.

## Configure

Follow the [Salesforce Connector setup](../../../README.md), then configure the
`salesforce / salesforce-crm` connection under **Connections** in Dex Web. The
record-sync example uses the same connection.

- **Poll interval** on the connection is the wait between polls, at least 5
  seconds. Every poll spends one request of the org's 24-hour API allocation,
  shown at Setup > Company Information > API Requests, Last 24 Hours.
- **Poll page size** on the connection is the SOQL `LIMIT` of one poll request,
  1 to 500.
- **Watched object** (`sObjectPicker`, required) on the `record-changes`
  Trigger binding is the object to watch. Choose a standard object or enter a
  custom object's API name from Setup > Object Manager > the object >
  API Name, such as `Invoice__c`.
- **Delivered fields** (`fieldNamesInput`) lists up to 50 field API names from
  Setup > Object Manager > the object > Fields & Relationships > Field Name,
  such as `Name, StageName`. Blank delivers only `Id`, `CreatedDate`,
  `LastModifiedDate`, and `SystemModstamp`.

The connected user needs read access to the object and every listed field. The
Worker reads the saved values once at startup, so restart it after saving. A
changed object records a new baseline; a changed field list keeps the cursor.

## Run

Generate strict FDG 2.0 from `connectors/salesforce/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/record-changes/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/record-changes
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/record-changes
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/record-changes/...
GOWORK=off go test -tags=integration ./examples/record-changes/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for the Salesforce REST query and sObject Rows resources. They check
that the baseline starts no Flow, that each change starts exactly one Flow,
that a bulk load sharing one `SystemModstamp` pages by `Id`, that a
`REQUEST_LIMIT_EXCEEDED` pause loses nothing, that a record deleted before the
read-back completes as deleted, and that changes made while the Trigger process
is stopped arrive after a restart. They also check that changing the object
records a new baseline. Live Salesforce polling has not been verified.
