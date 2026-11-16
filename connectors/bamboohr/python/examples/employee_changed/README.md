# BambooHR employee-changed example for Python

This example starts one `BambooHREmployeeChangePython` Flow for every employee
that BambooHR lists as inserted, updated, or deleted, through the
`employeeChanged` Trigger's `employee-changes` binding:

1. `RecordEmployeeChange` validates the delivered change and records it. A
   deleted employee completes the Flow at once, because BambooHR no longer
   returns the record.
2. `ReadChangedEmployee` calls `new_get_employee_step` for the hand-off fields
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

From `connectors/bamboohr/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```bash
mkdir -p build
uv run dexcli visualize ./examples/employee_changed/flow.py --schema-version 2.0 --json --out ./build/employee_changed
dexcli dev -flow-rendering-dir ./build
```

Open Dex Web at `http://127.0.0.1:8802`, configure the connection, set
`DEX_CONNECTOR_CONFIG_FILE` to the `connections.json` that Dex Web wrote, and
run the Worker, which also runs the Trigger:

```bash
uv sync --locked
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.employee_changed.worker
```

Set `DEX_BLOB_CACHE_DIR` or `LOG_LEVEL` when needed. Trigger records, without
employee data, API keys, or cursor tokens, go to the
`dex_connectors.sdk.trigger` logger.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change, and Go and Python Workers share one cursor. Under
`dexcli dev`, the poll cursor stays in process memory, as in Go, so a restarted
local Worker starts again at the current time.

## Test

```bash
uv run pytest tests/test_employee_changed_example.py
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/employee_changed/integration_test.py
```

The integration tests run a real Dex Worker and Client against a fake of
BambooHR's Get Changed Employee IDs and Get Employee APIs, with project storage
shared by two Trigger processes. They check that changes before the seed start
no Flow, that each inserted, updated, and deleted employee starts exactly one
Flow, and that changes made while the Trigger process is stopped arrive after a
restart. They also check that a saved `inserted` change type filters
BambooHR's list and that a 400 rejecting the cursor starts again at the current
time without delivering the gap. Live BambooHR polling has not been verified.
