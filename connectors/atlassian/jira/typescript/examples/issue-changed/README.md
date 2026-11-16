# Jira issue-changed example (TypeScript)

This example starts one `JiraIssueChangeTypeScript` Flow for every issue created
or updated in the picked project, through the `issueChanged` Trigger, at parity
with the [Go example](../../../go/examples/issue-changed/README.md):

1. `RecordIssueChange` validates the delivered change and records it.
2. `ReadChangedIssue` calls `newGetIssueStep` with the issue's stable ID, so the
   record holds the issue's current state.
3. `CompleteIssueChange` records the issue read back and completes the Flow.

The Flow ID is `jira-issue-change-<Trigger event ID>`, and the event ID is
stable per issue version, so a redelivered change finds its existing Flow. Only
the happy-path `found` branch is wired; a missing, rejected, or invalid read
fails the Flow. The first start records the project's existing issues as a
baseline and starts no Flow for them.

## Configure

Follow the [Jira Connector setup](../../../README.md), then configure the
`jira-issue-watcher` connection in Dex Web **Connections**. **Poll interval**
is the delay between polls, at least 5 seconds; each poll normally makes one
search request, which spends the app's hourly Jira rate-limit points. The
**Watched project** picker on the `project-issue-changes` binding lists the
projects the account can browse and saves the project ID, key, and name; leave
it unsaved to watch every project the account can browse. The Worker reads
saved values at startup, so restart it after saving; a changed project records
a new baseline.

## Run

From `connectors/atlassian/jira/typescript`, generate and lint strict FDG 2.0:

```sh
npm run build
npm install --prefix examples/issue-changed --no-save --no-package-lock --ignore-scripts .
cd examples/issue-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/issue-changed --open=false
```

Start `dexcli dev -flow-rendering-dir ./build/issue-changed`, then run the
Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json npm run example:changes
```

[`app.ts`](app.ts) wires the binding to the Flow:

```ts
const runner = newProjectIssueTriggerRunner(project, ConnectionName, [{
  bindingName: IssueChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryIssueChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
}], jiraOptions);
```

With hosted project storage, each change is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats a change. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline and changes made while it
was stopped are not delivered. Set `LOG_LEVEL=debug` to see every committed
poll and delivery.

## Test

`test/issue_changed_workflow.test.ts` covers the Flow's mapping, validation,
binding declaration, and project wiring. With `dexcli dev` running, the
integration tests run this application against a real Dex Worker and Client
and a local stand-in for Jira's enhanced search: the baseline starts no Flow,
each issue version starts one Flow across a restart, a 429 with `Retry-After`
pauses polling without losing a change, and a changed project records a new
baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Live Jira search has not been verified.
