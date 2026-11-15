# BambooHR employee-changed example for TypeScript

This example starts one `BambooHREmployeeChangeTypeScript` Flow for every
employee that BambooHR lists as inserted, updated, or deleted, through the
`employeeChanged` Trigger's `employee-changes` binding:

1. `RecordEmployeeChange` validates the delivered change and records it. A
   deleted employee completes the Flow at once, because BambooHR no longer
   returns the record.
2. `ReadChangedEmployee` calls `newGetEmployeeStep` for the hand-off fields
   (`firstName`, `lastName`, `workEmail`, `status`, `department`, `jobTitle`,
   `terminationDate`), because a change carries no field values.
3. `CompleteEmployeeChange` records the employee read back and completes. The
   `notFound` branch is also wired: an employee deleted between the change and
   the read is recorded as gone, and its Deleted change starts its own Flow.

The Flow ID is `bamboohr-employee-change-<Trigger event ID>`, and the event ID
is `employee-<id>-<action>-<lastChanged Unix seconds>`, so a redelivered change
finds its existing Flow. The first start records BambooHR's current time and
starts no Flow for earlier changes. See the
[connector README](../../../README.md#employeechanged-trigger) for change
coalescing, cursor resets, and rate limits.

## Configure

Follow the [BambooHR setup](../../../README.md#bamboohr-setup), then configure
the `bamboohr-company` connection under **Connections** in Dex Web.

- **pollInterval** on the connection is the delay between polls, at least 10
  seconds. Each poll makes one request per binding.
- **Employee changes** on the `employee-changes` binding is the
  `changeTypePicker` unit: **Every change**, **Inserted** (new hires),
  **Updated** (including terminations), or **Deleted**. It saves `inserted`,
  `updated`, or `deleted` as `/changeType`; an unsaved binding delivers every
  change. BambooHR derives the changes from the employees the API key's user
  can see, so the unit needs no BambooHR value.

The Worker reads the saved values once at startup, so restart it after saving.
A changed change type starts again from the current time.

## Run

From `connectors/bamboohr/typescript`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```bash
npm ci && npm run build
npm install --prefix examples/employee-changed --no-save --no-package-lock --ignore-scripts .
mkdir -p build
(cd examples/employee-changed && dexcli visualize flow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/employee-changed --open=false)
dexcli dev -flow-rendering-dir ./build
```

The local install lets the analyzer read the package's `dex-connector.json`.

Open Dex Web at `http://127.0.0.1:8802`, configure the connection, set
`DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote, and
run the Worker, which also runs the Trigger:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  npm run example:employee-changed
```

Set `LOG_LEVEL=debug` to also log every delivery. Trigger records carry no
employee data, API keys, or cursor tokens.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change, and Go and TypeScript Workers share one cursor. Under
`dexcli dev`, the poll cursor stays in process memory, as in Go, so a restarted
local Worker starts again at the current time.

## Test

```bash
npx vitest run test/employee_changed_workflow.test.ts
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npx vitest run test/integration/employee-changed.integration.test.ts
```

The integration tests run a real Dex Worker and Client against a fake of
BambooHR's Get Changed Employee IDs and Get Employee APIs, with project storage
shared by two Trigger processes. They check that changes before the seed start
no Flow, that each inserted, updated, and deleted employee starts exactly one
Flow, and that changes made while the Trigger process is stopped arrive after a
restart. They also check that a saved `inserted` change type filters
BambooHR's list and that a 400 rejecting the cursor starts again at the current
time without delivering the gap. Live BambooHR polling has not been verified.
