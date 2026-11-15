# Jira issue-changed example

This example starts one `JiraIssueChange` Flow for every issue created or
updated in the picked project, through the `issueChanged` Trigger:

1. `RecordIssueChange` validates the delivered change and records it.
2. `ReadChangedIssue` calls `jira.NewGetIssueStep` with the issue's stable ID,
   so the record holds the issue's current state and plain-text description.
3. `CompleteIssueChange` records the issue read back and completes the Flow.

The Flow ID is `jira-issue-change-<Trigger event ID>`, and the Trigger event ID
is stable per issue version (issue ID and updated time). A redelivered change
therefore finds its existing Flow instead of starting a second one. Only the
happy-path `found` branch is wired. A missing, rejected, or invalid read fails
the Flow.

The first start records the project's existing issues as a baseline and starts
no Flow for them. See the
[connector README](../../../README.md#issuechanged-trigger) for change kinds,
the lookback, pagination, and rate limits.

## Configure

Follow the [Jira Connector setup](../../../README.md), then configure the
`jira-issue-watcher` connection under **Connections** in Dex Web.

- **Poll interval** on the connection is the delay between polls, at least 5
  seconds. Each poll normally makes one search request, which spends the app's
  hourly Jira rate-limit points.
- **Watched project** on the `project-issue-changes` Trigger binding lists the
  projects on the connection's Jira site that the account can browse. It saves
  the project ID, key, and name. Leave it unsaved to watch every project the
  account can browse.

The Worker reads the saved values once at startup, so restart it after saving.
A changed project records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/jira/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/issue-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/issue-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration, or the `DEX_CONNECTOR_CONFIG_FILE`
connections file that Dex Web writes under `dexcli dev`, as documented in
[`sdkgo/projectconfig`](../../../../../../sdkgo/projectconfig/README.md#application-loading):

```bash
go run ./examples/issue-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed.

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline. Changes made while it was
stopped are not delivered.

## Test

```bash
GOWORK=off go test ./examples/issue-changed/...
GOWORK=off go test -tags=integration ./examples/issue-changed/... -count=1 -v
```

The integration tests run a real Dex Worker and Client against a local
stand-in for Jira's enhanced JQL search. They check that the baseline starts no
Flow, that each issue version starts exactly one Flow, and that changes made
while the Trigger process is stopped arrive after a restart. They also check
that a 429 with `Retry-After` pauses polling without losing a change, and that
a changed project records a new baseline. Live Jira search has not been
verified.
