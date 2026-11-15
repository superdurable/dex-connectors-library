# Asana task-changed example

This example starts one `AsanaTaskChange` Flow for every task event on the
picked project, through the `taskChanged` Trigger: a task or subtask added,
changed, removed, deleted, or restored.

1. `RecordTaskChange` validates the delivered event and records it. A deleted
   task completes the Flow at once, because Asana no longer serves it.
2. `ReadChangedTask` calls `asana.NewGetTaskStep`, so the record holds the
   task's current state. Asana events name the change but carry no task state.
3. `CompleteTaskChange` records the task read back and completes the Flow.

The Flow ID is `asana-task-change-<Trigger event ID>`, and the Trigger event ID
is stable for each Asana event. A redelivered event therefore finds its existing
Flow instead of starting a second one. Only the happy-path `found` branch is
wired. A task the connection can no longer see, such as one moved to a private
project, fails its Flow at `ReadChangedTask`.

The first start records the project's current position as a baseline and
starts no Flow for existing tasks. See the
[connector README](../../../README.md#taskchanged-trigger) for actions, expired
sync tokens, rate limits, and Asana's at-most-once event stream.

## Configure

Follow the [Asana Connector setup](../../../README.md#authentication), then
configure the `asana-task-watcher` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Each poll is one request against the token user's per-minute quota.
- **Watched project** on the `project-task-changes` Trigger binding lists the
  workspaces of the token's user, then that workspace's unarchived projects. It
  saves the workspace ID, project ID, and project name. A project is required.
  Without the lists, enter the project gid, the long number after `/project/`
  in the project's Asana web address.

The Worker reads the saved values once at startup, so restart it after saving.
A changed project records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/asana/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/task-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/task-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/task-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each event is recorded in the binding's Trigger
inbox before the sync token commits, so a restarted Worker neither misses nor
repeats an event while Asana still holds it (24 hours). Under `dexcli dev`, the
poll cursor stays in process memory, so a restarted local Worker records a new
baseline. Events made while it was stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/task-changed/...
GOWORK=off go test -tags=integration ./examples/task-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for the Asana Events API. They check that the baseline starts no
Flow, that each task event starts exactly one Flow and comment stories start
none, and that events made while the Trigger process is stopped arrive after a
restart whose first poll is rate limited. They also check that an expired sync
token (`412`) records a new baseline without delivering the gap. The live Asana
Events API has not been verified.
