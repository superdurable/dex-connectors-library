# Record-changed intake example

This example starts one Flow per record created or changed in a picked
Airtable table, the Python counterpart of the Go
[`record-changed-intake`](../../../go/examples/record-changed-intake) example:

1. the `recordChanged` Trigger polls the `intake-record-changed` binding's
   table by its Last modified time field;
2. each new change is recorded in the binding's durable project inbox;
3. a Flow-start target starts `AirtableRecordChangedIntakePython` with the
   Flow ID `airtable-record-change-<event ID>`;
4. `RecordChangedRecord` persists the record ID, modification time, and
   fields in the `airtable-record-intake` Attribute;
5. `ReadBackChangedRecord`, the connector's `getRecord` Step, reads the record
   back through the same `airtable-intake` connection, and
   `CompleteRecordIntake` stores its current fields and completes the Flow, or
   `RecordRemovedRecord` completes it as removed when the record was deleted
   in the meantime.

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
`intake-record-changed` binding with **Base**, **Table**, and **Last modified
time field**. If Dex Web shows no pickers for the Python binding, save its
configuration as JSON: `baseId` is the `app…` segment of the base's
`https://airtable.com/` address, `tableId` the `tbl…` segment, and `fieldId`
the `fld…` ID that the base's API documentation at
`https://airtable.com/<baseId>/api/docs` lists for the field.

The connection's `pollInterval` and `pollPageSize` settings apply to every
binding of the connection. Each poll normally makes one Airtable list request;
the first poll also reads the base schema. Each Flow makes one more request to
read its record back.

With Dex CLI 1.47.3 or later, generate strict FDG 2.0 from
`connectors/airtable/python` after `uv sync --locked`:

```sh
mkdir -p build
dexcli visualize ./examples/record_changed_intake/flow.py --language python \
  --python .venv/bin/python --schema-version 2.0 --json \
  --out ./build/airtable-record-changed-intake
```

Run `dexcli dev -flow-rendering-dir ./build`, export the project's
`DEX_CONNECTOR_CONFIG_FILE` and `DEX_FLOW_SERVICE_ADDRESS`, then run the Worker:

```sh
uv run python -m examples.record_changed_intake.main
```

`LOG_LEVEL=DEBUG` also shows each cursor commit and delivery. Records carry
the connector, connection, Trigger, and binding, never field values, field
names, or tokens. Under `dexcli dev` the poll cursor stays in process memory, so
a restart seeds again; hosted project storage keeps it.

## Test

`uv run pytest tests/test_record_changed_intake_example.py` checks the Flow's
mapping, Flow IDs, read-back input and completion, and binding declaration. With `dexcli dev` running:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/record_changed_intake/integration_test.py
```

The real-Dex tests serve a fake Airtable table and check that existing records
start no Flow, each created or edited record starts exactly one completed Flow
that read the record back through the connection,
a renamed field keeps working, a change made while the runner is stopped
arrives after a restart that first answers 503, and the Worker entrypoint's
runner polls the binding saved in the project configuration.
