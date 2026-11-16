# Record-changed intake example

This example starts one Flow per record created or changed in a picked
Airtable table, the TypeScript counterpart of the Go
[`record-changed-intake`](../../../go/examples/record-changed-intake) example:

1. the `recordChanged` Trigger polls the `intake-record-changed` binding's
   table by its Last modified time field;
2. each new change is recorded in the binding's durable project inbox;
3. a Flow-start target starts `AirtableRecordChangedIntakeTypeScript` with the
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
time field**. The connection's `pollInterval` and `pollPageSize` settings apply
to every binding of the connection. Each poll normally makes one Airtable list
request; the first poll also reads the base schema. Each Flow makes one more
request to read its record back.

Generate a strict Flow Definition Graph 2.0 with Dex CLI 1.47.3 or later. The
analyzer resolves `@superdurable/connector-airtable` from `node_modules`, so
install the local build next to the example first, from
`connectors/airtable/typescript`:

```sh
npm ci
npm run build
npm install --prefix examples/record-changed-intake --no-save --no-package-lock --ignore-scripts .
cd examples/record-changed-intake
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out - --open=false
```

Start `dexcli dev`, set `DEX_FLOW_SERVICE_ADDRESS` and the Dex project
environment for the connection, then run the Worker:

```sh
npm run example:record-changed
```

[`runner.ts`](runner.ts) builds the durable runner; [`main.ts`](main.ts) opens
the project connection for the Flow's read-back, serves the Worker, and polls
until interrupted. Under `dexcli dev` the poll cursor
stays in process memory, so a restart seeds again; hosted project storage keeps
it. Log records carry the connector, connection, Trigger, and binding, never
field values, field names, or tokens.

## Test

`test/record_changed_intake_workflow.test.ts` checks the Flow's mapping, Flow
IDs, read-back input and completion, and binding declaration. With `dexcli dev` running,
`DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:<dex-port> npm run test:integration` runs
[`record-changed.integration.test.ts`](../../test/integration/record-changed.integration.test.ts)
against real Dex and a fake Airtable table: existing records start no Flow,
each created or edited record starts exactly one completed Flow that read the
record back through the connection, a renamed
field keeps working, a change made while the runner is stopped arrives after a
restart that first answers 503, and this example's runner polls the binding
saved in the project configuration. Live Airtable authorization and provider
behavior remain unverified.
