# Airtable record-changed-intake example

This example starts one Flow per record created or changed in a picked
Airtable table:

1. the `recordChanged` Trigger polls the `intake-record-changed` binding's
   table by its Last modified time field;
2. each new change is recorded in the binding's durable project inbox;
3. a Flow-start target starts `AirtableRecordChangedIntake` with the Flow ID
   `airtable-record-change-<event ID>`;
4. `RecordChangedRecord` persists the record ID, modification time, and fields;
5. the `ReadBackChangedRecord` Airtable `getRecord` Step reads the record by its
   ID through the same `airtable-intake` connection;
6. `CompleteRecordIntake` stores the record's current fields and completes the
   Flow, or `RecordRemovedRecord` completes it as removed when the record was
   deleted before the read.

Records already in the table when the Worker first polls never start Flows.
Editing a record again later starts a new Flow for the new change.

## Prepare the table

In Airtable, open the table, add a field, choose the type **Last modified
time**, and turn on **Include time**. Leave it watching **All editable fields**
to react to any edit, or pick specific fields, such as `Status`, to react only
to those.

## Configure

Follow the [Airtable Connector setup](../../../README.md#authentication); the
token needs `data.records:read` and `schema.bases:read` and access to the base.
Then in Dex Web **Connections** configure `airtable / airtable-intake` and its
`intake-record-changed` binding:

- **Base** lists the bases the token can reach and stores the base ID.
- **Table** lists the base's tables and stores the table ID.
- **Last modified time field** lists the table's Last modified time fields
  with a time of day and stores the field ID.

The connection's `pollInterval` and `pollPageSize` settings apply to every
binding of the connection. Each poll normally makes one Airtable list request;
the first poll also reads the base schema, and each
started Flow makes one get request. Airtable allows 5 requests per
second per base and 50 per second per token.

Generate strict FDG 2.0 from `connectors/airtable/go`:

```bash
mkdir -p build
dexcli visualize ./examples/record-changed-intake/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/record-changed-intake
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/record-changed-intake
```

## Delivery and logs

The runner commits the binding's cursor, the latest delivered modification
time and the records delivered in the five seconds before it, only after the inbox recorded
every change before it, so a restart or a second replica neither misses nor
replays a change. Renaming the Last modified time field is picked up on the
next poll. Picking a different field reseeds the cursor and logs
`poll cursor invalid; reseeding`; changes in that gap are not delivered.

HTTP 429 pauses every binding of the connection for at least Airtable's 30
seconds; other failed polls back off up to five minutes. Records carry the
connector, connection, Trigger, and binding, never field values, field names,
or tokens. Set `LOG_LEVEL=debug` to also see each cursor commit and delivery.

## Test

```bash
GOWORK=off go test -race ./examples/record-changed-intake/...
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/record-changed-intake/...
```

The real-Dex test in
[`record_changed_integration_test.go`](flow/record_changed_integration_test.go)
serves a fake Airtable table and checks that existing records start no Flow,
each created or edited record starts exactly one Flow that reads the record
back and completes with its current fields, a renamed
field keeps working, and a change made while the runner is stopped arrives
after a restart that first answers 503.
