# BambooHR employee change sweep example

This example runs one operation-only Flow from Dex Web **Start Flow**. It
reads BambooHR's employee change history, the poll source for onboarding and
offboarding while the connector has no Triggers:

1. `RecordSweepWindow` validates the RFC 3339 instant to sweep from, an
   optional employee ID tie-breaker, the change type, the page size, and the
   page limit, and records the sweep.
2. `ListEmployeeChanges` calls `bamboohr.NewListEmployeeChangesStep` for the
   oldest changes after the cursor.
3. `CollectEmployeeChanges` keeps the page's changes and reads the next page
   while BambooHR reports more and pages remain, then completes with every
   change read, the cursor the next sweep starts from, and whether the sweep
   caught up.

Every optional branch is unwired and fails the Flow, such as rejected
credentials or an oversized history. The Flow only reads BambooHR.

Store the result's `nextCursor` and pass `nextCursor.since` and
`nextCursor.afterEmployeeId` to the next sweep: the cursor carries the last
employee ID at its instant, so changes that share a second are neither
skipped nor repeated. `changeType: inserted` lists the employees added, the
hires for the [new-hire onboarding](../new-hire-onboarding) check; `updated`
includes terminations, which BambooHR records as updates.

## Generate the Flow Definition

From `connectors/bamboohr`, with the dexcli release pinned in the repository's
`.dex-compat-version` file:

```bash
mkdir -p build
dexcli visualize ./examples/employee-change-sweep/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/employee-change-sweep
```

The command must report `valid: true` with the expected
`connector_release_required` warning on the connector Step.

## Configure and run

Build the release override and start `dexcli dev` as the
[new-hire onboarding](../new-hire-onboarding/README.md#configure-and-run)
example describes, then configure `bamboohr / bamboohr-company` in Dex Web
**Connections**; both examples share that connection. Start the Worker from
`connectors/bamboohr`:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/employee-change-sweep
```

The default Worker address is `127.0.0.1:8837`; `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `DEX_BLOB_CACHE_DIR`, and, for a local fake only,
`BAMBOOHR_LOCAL_API_BASE_URL` work as in the onboarding example.

In the Run workspace, choose **Start Flow**, select
`BambooHREmployeeChangeSweep`, choose the Worker at `127.0.0.1:8837`, enter a
unique Flow ID, and submit:

```json
{
  "since": "2026-09-24T00:00:00Z",
  "changeType": "inserted",
  "pageSize": 100,
  "maxPages": 5
}
```

Leave `changeType` empty to list every change, `pageSize` empty for 100 per
page, and `maxPages` empty for five pages; a sweep reads at most ten pages.
The Flow result and the `bamboohr-employee-change-sweep` Attribute hold the
changes, the pages read, the next cursor, and `isCaughtUp`, which is false
when the page limit stopped the sweep.

## Test

```bash
GOWORK=off go test -race ./examples/employee-change-sweep/...
```

With the pinned Dex development server running, the integration tests drive
the Flow on a real Worker against a fake change history that treats `since`
as exclusive:

- a sweep of new hires reads three pages, skips a change before the window,
  and keeps two hires added in the same second;
- four sweeps that each stop at their page limit continue from the previous
  cursor with no gap or duplicate, across a page boundary inside one second;
- a listing held for nine seconds is dispatched again by async Dex and keeps
  each change once;
- a rate-limited listing waits for `Retry-After`;
- rejected credentials fail the Flow through the unwired `providerRejected`
  branch.

```bash
GOWORK=off go test -tags=integration ./examples/employee-change-sweep/... -count=1 -v
```
