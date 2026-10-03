# Jira issue triage example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every Jira operation:

1. `RecordTriageRequest` validates the request and records it with the picked
   project;
2. `FindOpenDuplicateIssues` calls `jira.NewSearchIssuesStep` with a typed
   filter for open issues in the project whose summary contains the requested
   summary, and `EvaluateDuplicateIssues` reuses an issue whose summary is
   exactly the same. A near-duplicate such as `Fire panel wiring (2025)` is not
   reused. Search is eventually consistent, so this is a business duplicate
   check, never the retry-safety mechanism;
3. `CreateTriageIssue` calls `jira.NewCreateIssueStep` with sync durability.
   `created` records the key, `providerRejected` completes as `rejected` with
   the field IDs, and `uncertain` parks the Flow for an operator;
4. `ReadBackTriageIssue` calls `jira.NewGetIssueStep`, and `VerifyTriageIssue`
   adopts the issue;
5. `AddTriageComment` calls `jira.NewAddCommentStep`. Both `added` and
   `uncertain` continue; an uncertain comment is recorded and never re-sent;
6. `MoveTriageIssue` calls `jira.NewTransitionIssueStep` by destination status,
   so a retried or backup attempt recognizes a move Jira already made.
   `transitionUnavailable` completes with the transitions Jira offered.

A blank `triageComment` skips the comment and a blank `destinationStatusName`
skips the transition. Unwired optional branches, such as a rejected search, a
missing issue after a create, or a rejected transition, fail the Flow.

## Reconciling an uncertain create

`uncertain` means the issue may exist. The Flow records the connector Call ID
and the time the outcome was observed, sets the phase to `needsReconciliation`,
and waits. An operator searches the project for the summary around that time,
then runs one of two Actions, each requiring `jira-issue-triage.reconcile`:

- **Confirm created issue** takes the key the operator found. The Flow reads it
  with `getIssue` and adopts it only when its project and summary match;
  otherwise it returns to the operator with a note.
- **Create issue again** is the only path that creates again, as a new Step
  execution with a new connector Call ID.

## Configure

Follow the [Jira connector setup](../../README.md), then configure the
`jira-triage` connection under **Connections** in Dex Web. Leave `cloudId` blank
when the authorization covers one Jira site. The **Triage project** picker on
the `CreateTriageIssue` Step saves the project; every other Step uses the same
project. Leave it unsaved to use each Start Flow input's `projectKey`. The
Worker reads the saved value once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/jira` with the latest stable
dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/triage-issue/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/triage-issue
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/triage-issue
```

The default Worker address is `127.0.0.1:8830`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `JiraIssueTriage` with a unique Flow ID:

```json
{
  "summary": "Fire panel wiring",
  "description": "Panel B trips at 02:00.\nBreaker is warm.",
  "projectKey": "OPS",
  "labels": ["facilities"],
  "triageComment": "Paging the on-call electrician.",
  "destinationStatusName": "In Progress"
}
```

The Flow result and the `jira-issue-triage` Attribute hold the phase, the
read-back issue, the comment ID, and the final status.

## Test

```bash
GOWORK=off go test -race ./examples/triage-issue/...
```

With the latest Dex development server running, the integration test drives
the Flow on a real Worker against a stateful fake Jira: a new issue created
once beside a near-duplicate and read back, commented, and moved; an open
issue with the same summary reused; a rejected create; a rate-limited create
retried after `Retry-After`; a create timeout reconciled by confirming a
missing, a mismatched, and then the real issue key without a second create; a
5xx create created again only after operator approval; a nine-second create
sent once under sync durability; a nine-second transition whose backup attempt
finds the issue already moved and sends nothing; a destination the workflow does
not offer; a comment timeout recorded without a resend; and a blank `cloudId`
resolved to the only granted site.

```bash
GOWORK=off go test -tags=integration ./examples/triage-issue/... -count=1 -v
```
