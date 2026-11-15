# BambooHR employee-changed example

This example starts one `BambooHREmployeeChange` Flow for every employee that
BambooHR lists as inserted, updated, or deleted, through the `employeeChanged`
Trigger:

1. `RecordEmployeeChange` validates the delivered change and records it. A
   deleted employee completes the Flow at once, because BambooHR no longer
   returns the record.
2. `ReadChangedEmployee` calls `bamboohr.NewGetEmployeeStep` for the hand-off
   fields (`firstName`, `lastName`, `workEmail`, `status`, `department`,
   `jobTitle`, `terminationDate`), because a change carries no field values.
3. `CompleteEmployeeChange` records the employee read back and completes the
   Flow.

The Flow ID is `bamboohr-employee-change-<Trigger event ID>`, and the Trigger
event ID is `employee-<id>-<action>-<lastChanged Unix seconds>`. A redelivered
change therefore finds its existing Flow instead of starting a second one.
Besides `found`, the read wires `notFound` as a business reaction: an employee
deleted between the change and the read is recorded as gone, and its Deleted
change starts its own Flow. A rejected or invalid read fails the Flow.

The first start records the current time and starts no Flow for earlier
changes. See the [connector README](../../../README.md#employeechanged-trigger)
for change coalescing, cursor resets, and rate limits.

## Configure

Follow the [BambooHR setup](../../../README.md#bamboohr-setup), then configure
the `bamboohr-company` connection under **Connections** in Dex Web; the other
BambooHR examples share it.

- **pollInterval** on the connection is the delay between polls, at least 10
  seconds. Each poll makes one request per binding.
- **Employee changes** on the `employee-changes` Trigger binding is the
  `changeTypePicker` unit: **Every change**, **Inserted** (new hires),
  **Updated** (including terminations), or **Deleted**. It saves `inserted`,
  `updated`, or `deleted` as `/changeType`; an unsaved binding delivers every
  change. BambooHR derives the changes from the employees the API key's user
  can see, so the unit needs no BambooHR value.

The Worker reads the saved values once at startup, so restart it after saving.
A changed change type starts again from the current time.

## Run

Generate strict FDG 2.0 from `connectors/bamboohr/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/employee-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/employee-changed
```

The command must report `valid: true` with the expected
`connector_release_required` and `connector_trigger_release_required`
warnings, because a local module is not a published release.

Build the release override, including the Studio bundle from `ui/`, and start
`dexcli dev` as the
[new-hire onboarding](../new-hire-onboarding/README.md#configure-and-run)
example describes. Then run the Worker from `connectors/bamboohr/go`. It reads
the `DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/employee-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`), or, for a local fake only,
`BAMBOOHR_LOCAL_API_BASE_URL` when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker starts again from the current time, and changes
made while it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/employee-changed/...
GOWORK=off go test -tags=integration ./examples/employee-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for BambooHR's changed-employees list and employee read. They check
that existing employees start no Flow, that an insert, a termination update,
and a delete each start exactly one Flow, and that changes made while the
Trigger process is stopped arrive after a restart whose first request answers
429 with `Retry-After`. They also check that a `400` for the stored cursor
starts again at the current time without delivering the gap. Live BambooHR
polling has not been verified.
