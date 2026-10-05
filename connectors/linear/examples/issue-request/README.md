# Linear issue-request example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses every Linear operation to
handle an issue request:

1. `RecordIssueRequest` validates the request, uses the team saved with the `Request team` picker or else the
   input's `teamId`, and records the request;
2. when `assigneeEmail` is set, `FindAssignee` calls `linear.NewFindUserByEmailStep`; `RecordAssignee` keeps
   the user's UUID, or records `assigneeUnknown` and leaves the assignee unchanged;
3. `FindOpenIssue` calls `linear.NewSearchIssuesStep` for the team's issues whose title is exactly the
   requested title and whose state type is `triage`, `backlog`, `unstarted`, or `started`.
   `ChooseOpenIssue` reuses the first such issue, or goes on to create one;
4. `CreateRequestedIssue` calls `linear.NewCreateIssueStep` with the title, description, and resolved
   assignee. Its `providerRejected` branch goes to `RecordRejectedIssue`, which records the connector's
   credential-free reason and fails the Flow;
5. `ListTeamStates` calls `linear.NewListWorkflowStatesStep`, and `ChooseTargetState` picks the state named
   `stateName`, or the team's first `unstarted` state, failing with the available names when none matches;
6. `MoveIssue` calls `linear.NewUpdateIssueStep` to set the state and the resolved assignee;
7. `AddIssueComment` calls `linear.NewAddCommentStep`, and `ReadBackIssue` calls `linear.NewGetIssueStep`
   before `CompleteIssueRequest` completes with the outcome.

Every other optional branch is unwired and fails the Flow, such as an unknown team, a missing issue, an
invalid response, or a local defect. The create and the comment carry the Step's client-supplied UUID, and the
update sets absolute values, so each keeps async durability; a repeated dispatch writes nothing twice as long
as Linear refuses a second record with an existing ID, which the stand-in does and Linear was not tested for.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/linear` with the latest stable dexcli release:

```bash
dexcli visualize ./examples/issue-request/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/linear-issue-request
```

The command must report `valid: true`. Inside this repository it also warns `connector_release_required` for
each connector Step, because a local module is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata and the Studio bundle built from
this source, passed as an override; without it the connection shows **Unsupported**. From the repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/linear-release
(cd sdk/react && npm ci && npm run build)
(cd connectors/linear/ui && npm ci && npm run build)
go run ./cmd/connectorctl ui-artifact \
  --manifest connectors/linear/connector.yaml \
  --ui-root connectors/linear/ui/dist \
  --output /tmp/linear-release/connector-ui.tgz \
  --digest-output /tmp/linear-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/linear/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/linear \
  --version v0.21.0 --tag connectors/linear/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/linear-release/connector-ui.tgz \
  --ui-digest /tmp/linear-release/connector-ui.tgz.sha256 \
  --output /tmp/linear-release/connector-release.json \
  --digest-output /tmp/linear-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-release-override linear=/tmp/linear-release
```

Follow the [Linear setup](../../README.md#linear-setup), then open **Connections** in Dex Web and select
`linear / linear-workspace`, which all seven Linear Steps of `LinearIssueRequest` use. Choose **Personal API
key** and paste the `lin_api_` key, or **Linear OAuth** and authorize, and save; the status becomes **Ready**.

Then configure the `CreateRequestedIssue` Step's **Request team** unit. With an OAuth connection, choose
**Load teams** and pick the team; with a personal API key, paste the team UUID from Linear **Settings > Teams >
the team > Copy team ID**. Save, and restart the Worker, which reads the pick at startup. Leaving the unit
unsaved makes every request name `teamId` instead.

In a second terminal, start the Worker from `connectors/linear`. It reads the `DEX_PROJECT_*` project
configuration environment described in [project configuration](../../../../sdkgo/projectconfig/README.md); Dex
Web or Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/issue-request
```

The default Worker address is `127.0.0.1:8836`. Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`,
or `DEX_BLOB_CACHE_DIR` when needed. For local verification against a Linear-compatible fake only,
`LINEAR_LOCAL_API_URL` replaces `https://api.linear.app/graphql`; it must be HTTPS or a loopback HTTP URL, and
production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `LinearIssueRequest`, choose the Worker at
`127.0.0.1:8836`, enter a unique Flow ID, and submit:

```json
{
  "title": "Monthly Fire Drill Checklist - February",
  "description": "Check every panel on **all** floors.",
  "assigneeEmail": "alice@example.com",
  "stateName": "In Progress",
  "comment": "Scheduled from the facilities request."
}
```

Add `"teamId": "<team UUID>"` when the picker is unsaved. Omit `stateName` to use the team's first unstarted
state, and `assigneeEmail` to keep the assignee. The Flow result and the `linear-issue-outcome` Attribute hold
the action (`created` or `reused`), the issue ID, identifier, and URL, the assignee, the state, the comment ID,
and the state type read back. The run writes to the real workspace; delete the test issue afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/issue-request/...
```

With the latest Dex development server running, the integration tests drive the Flow on a real Worker against
a stateful fake Linear that stores client-supplied IDs and refuses to store one twice:

- a new request resolves the assignee, creates one issue, moves it to `In Progress`, and adds one comment;
- the team's open issue with the title is reused, and these decoys stay untouched: a completed issue with the
  title, one in another team, and a look-alike title;
- an unknown assignee email leaves the issue unassigned, and a blank state name picks `Todo`;
- a create, a comment, and an update held for nine seconds, past Dex's async local phase, are each dispatched
  again; the repeated create and comment meet the stored UUID and read the record back, so Linear holds one
  issue and one comment;
- a Worker lost while Linear holds the create is replaced, and the new Worker's attempt sends the same UUID and
  reads back the first issue;
- a search answered with Linear's HTTP 400 `RATELIMITED` and `Retry-After: 1` is retried;
- a rejected create fails the Flow through `RecordRejectedIssue` without Linear's message text, after one
  read-back that finds no issue;
- a request without a team fails before any Linear request.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/issue-request/... -count=1 -v
```
