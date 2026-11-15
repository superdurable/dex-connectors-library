# HubSpot crm-object-changed example

This example starts one `HubSpotCRMObjectChange` Flow for every contact,
company, or deal of the picked object type that is created or changed, through
the `crmObjectChanged` Trigger:

1. `RecordCRMObjectChange` validates the delivered change and records it.
2. `ReadChangedObject` calls `hubspot.NewGetObjectStep` with
   `DeliveredProperties`, so the outcome holds the record's current values
   rather than the search page's copy.
3. `CompleteCRMObjectChange` records the record read back and completes the
   Flow.

The Flow ID is `hubspot-crm-object-change-<Trigger event ID>`, and the Trigger
event ID is stable per record version. A redelivered change therefore finds its
existing Flow instead of starting a second one. Only the happy-path `found`
branch is wired. A record deleted before the read, or a rejected read, fails the
Flow.

The first start records the object type's existing records as a baseline and
starts no Flow for them. See the
[connector README](../../../README.md#crmobjectchanged-trigger) for the cursor,
indexing delay, change kinds, and rate limits.

## Configure

Follow the [HubSpot Connector setup](../../../README.md#authentication), then
configure the `hubspot-crm-watcher` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Shorter intervals deliver changes sooner and use more of the
  account's search rate limit and daily API limit.
- **Watched object type** on the `crm-object-changes` Trigger binding saves
  `contacts`, `companies`, or `deals`. It is required. The connection's private
  app or OAuth app needs the matching `crm.objects.<type>.read` scope, which the
  authorization guide already lists.

The Worker reads the saved values once at startup, so restart it after saving.
A changed object type records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/hubspot/go` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/crm-object-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/crm-object-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/crm-object-changed
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
GOWORK=off go test ./examples/crm-object-changed/...
GOWORK=off go test -tags=integration ./examples/crm-object-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for HubSpot CRM search and record reads. They check that the baseline
starts no Flow, that each change starts exactly one Flow, and that changes made
while the Trigger process is stopped arrive after a restart. They also check
that a 429 with `Retry-After` pauses polling and the change arrives after the
pause. Live HubSpot search has not been verified.
