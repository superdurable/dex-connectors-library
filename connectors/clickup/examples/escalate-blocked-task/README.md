# ClickUp escalate-blocked-task example

This example escalates a ClickUp task that moves to the blocked status. The
`taskEvent` Trigger starts one `ClickUpEscalateBlockedTask` Flow per status
change, and the Flow uses every ClickUp operation:

1. the Worker mounts `NewProjectTaskEventEndpointRunner` at
   `/webhooks/clickup` and starts it at once, so an event is recorded in the
   binding's durable project inbox even while Dex is unreachable;
2. the `blocked-status` binding accepts the events its `taskEventPicker`
   saved, and `AcceptBlockedStatusEvent` admits only a `taskStatusUpdated`
   whose new status is the configured blocked status;
3. `sdkgo.NewDexFlowTriggerTarget` starts the Flow with ID
   `clickup-escalation-<task ID>-<history item ID>` and the event ID as
   request ID, so a redelivery starts no second Flow while a later block of the
   same task starts a new one;
4. `RecordBlockedTask` validates the task ID and the escalation settings, and
   `ReadBlockedTask` calls `getTask`;
5. `ChooseEscalation` skips a task that is no longer blocked, and
   `FindEscalationManager` calls `findMemberByEmail`.
   `CompleteWithoutManager` completes as `skipped` when no Workspace member has
   the manager's email;
6. `SearchExistingEscalation` calls `searchTasks` for the escalation List's
   tasks tagged `escalation`, open or closed, and `ChooseExistingEscalation`
   reuses the one whose name ends with `[<task ID>]`, searches the next page (at
   most five), or asks for a new one;
7. `CreateEscalationTask` calls `createTask`, assigned to the manager
   with urgent priority. `RecordEscalationTask` records it, and
   `RecordCreateNeedsReview` completes as `needsReview` when the create's
   outcome cannot be confirmed;
8. `TagBlockedTask` calls `updateTaskTags` to add `escalated`,
   `AssignManagerToBlockedTask` calls `updateTask` to add the manager as an
   assignee and set urgent priority, and `CommentOnBlockedTask` calls
   `addComment` with the escalation task's link;
   `RecordCommentNeedsReview` completes as `needsReview` when the comment's
   outcome cannot be confirmed, and `CompleteEscalated` completes as
   `escalated`.

Only the happy-path branches, `notFound` of `findMemberByEmail`, and
`uncertain` of `createTask` and `addComment` are wired. A missing task, a
rejected request, an invalid response, and a local defect fail the Flow.

## 1. Generate the Flow Definition

From `connectors/clickup`, generate strict FDG 2.0 with the latest stable
dexcli release
(`python3 script/dex_compatibility.py install-dexcli --output <path>` installs
it):

```bash
mkdir -p build
dexcli visualize ./examples/escalate-blocked-task/flow/workflow.go \
  --schema-version 2.0 \
  --json \
  --out ./build/escalate-blocked-task
```

The command must report `valid: true` with the eighteen Steps and the
`blocked-status` binding. Inside this repository it also warns
`connector_release_required` and `connector_trigger_release_required`, because
a local module is not a published release.

## 2. Start Dex with this connector's release metadata

The connector has a Studio bundle, so build it and its release metadata from
this source; without them the connection shows **Unsupported**. From the
repository root:

```bash
cd "$(git rev-parse --show-toplevel)"
(npm ci --prefix sdk/react && npm run build --prefix sdk/react)
(cd connectors/clickup/ui && npm ci && npm run build)
mkdir -p /tmp/clickup-release
go run ./cmd/connectorctl ui-artifact --manifest connectors/clickup/connector.yaml \
  --ui-root connectors/clickup/ui/dist \
  --output /tmp/clickup-release/connector-ui.tgz --digest-output /tmp/clickup-release/connector-ui.tgz.sha256
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/clickup/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/clickup \
  --version v0.21.0 --tag connectors/clickup/v0.21.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --ui-artifact /tmp/clickup-release/connector-ui.tgz --ui-digest /tmp/clickup-release/connector-ui.tgz.sha256 \
  --output /tmp/clickup-release/connector-release.json \
  --digest-output /tmp/clickup-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "$PWD/connectors/clickup/build" \
  --connector-release-override clickup=/tmp/clickup-release
```

## 3. Configure the connection and the events

Follow the [ClickUp setup](../../README.md#authentication), then open the Dex
Web URL that dexcli prints, select **Connectors**, and select **ClickUp**
(connection `clickup-workspace`):

- `api_token`: the `pk_` personal API token of the user the Flow acts as;
- `webhook_secret`: the `webhook.secret` that Create Webhook returned in step 4.

Save the connection. Dex Web then shows this Flow's configuration unit,
**Events that start the Flow** on the `blocked-status` binding: select
`taskStatusUpdated` and save. Dex Web stores the binding beside the connection:

```json
{
  "connectorId": "clickup",
  "connectionName": "clickup-workspace",
  "triggerName": "taskEvent",
  "bindingName": "blocked-status",
  "configuration": {"events": ["taskStatusUpdated"]}
}
```

## 4. Run the Worker and register the webhook

The escalation rules are the application's own settings, read once at startup
from the environment:

| Variable | Meaning |
| --- | --- |
| `CLICKUP_WORKSPACE_ID` | The Workspace ID, the first number after `app.clickup.com/` in any ClickUp web address. |
| `CLICKUP_ESCALATION_LIST_ID` | The List escalation tasks are created in, the number after `/li/` when the List is open. |
| `CLICKUP_ESCALATION_MANAGER_EMAIL` | The email of the Workspace member who receives escalations. |
| `CLICKUP_BLOCKED_STATUS` | The status name that escalates a task, compared without case; blank means `blocked`. |

Dex Web or Superverse Studio writes the connection and the binding to the
project configuration. In a second terminal, from `connectors/clickup`, run
the Worker with those variables and the `DEX_PROJECT_*` environment that names
that configuration, as
[project configuration loading](../../../../sdkgo/projectconfig/README.md#application-loading)
describes:

```bash
cd "$(git rev-parse --show-toplevel)/connectors/clickup"
go run ./examples/escalate-blocked-task
```

The Worker listens on `127.0.0.1:8881` and the webhook endpoint on
`127.0.0.1:8882`; override `DEX_FLOW_SERVICE_ADDRESS`,
`DEX_WORKER_BIND_ADDRESS`, `CLICKUP_WEBHOOK_BIND_ADDRESS`, or
`DEX_BLOB_CACHE_DIR` when needed, and set `LOG_LEVEL=debug` to see every
delivery. `GET /readyz` answers `200` once the binding receives events. For
local verification against a ClickUp-compatible fake only,
`CLICKUP_LOCAL_API_BASE_URL` replaces the ClickUp API host; production Workers
leave it unset.

Expose `http://127.0.0.1:8882/webhooks/clickup` through an HTTPS tunnel and
register it as the connector README's [webhook section](../../README.md#taskevent-trigger)
shows, with `"events": ["taskStatusUpdated"]`. Copy `webhook.secret` into the
connection's `webhook_secret` and save; the Worker reads it for every request,
so no restart is needed. When someone moves a task to the blocked status, Dex
Web shows the completed run with this `clickup-escalation-outcome`:

```json
{
  "action": "escalated",
  "taskId": "86b2x4k7q",
  "escalationTaskId": "86b2x9z11",
  "escalationTaskUrl": "https://app.clickup.com/t/86b2x9z11",
  "managerId": 183,
  "commentId": "90140001234",
  "tags": ["escalated"]
}
```

A task that is no longer blocked when the Flow reads it completes as `skipped`
with reason `notBlocked`, and nothing is written to ClickUp. The Flow can also
be started from Dex Web **Start Flow** with `{"taskId": "86b2x4k7q"}`.

## 5. Redeliveries, forgeries, and other statuses

- ClickUp retries an event up to five times when the endpoint does not answer
  `2xx` within seven seconds. The endpoint answers `200`, and no second Flow
  starts: the event ID is the Flow-start request ID.
- An event whose `X-Signature` does not match is answered `400` and nothing is
  recorded. The endpoint never answers `401`, which would make ClickUp suspend
  the webhook.
- A status change to another status, and events the binding does not accept,
  are answered `200` and start nothing.

## 6. Restart recovery

Stop Dex, move a task to the blocked status, and stop the Worker. The endpoint
answered `200` because the event was stored; it stays in the binding's durable
inbox in project storage. Start Dex and the Worker again: the runner replays it,
logs `replaying pending trigger events`, and the Flow starts and completes.

## Test

From `connectors/clickup`:

```bash
go test -race ./examples/escalate-blocked-task/...
```

The unit tests cover the settings, the admission rule, the Flow identity, the
escalation task's name, the mappers, and the README samples.

With the latest Dex development server running, the real-Dex tests drive the
Flow on a real Worker against a stateful fake ClickUp that requires the raw
`pk_` token:

- a blocked task is escalated once, with the expected create and update
  requests, while a decoy escalation task is untouched;
- an escalation task filed earlier, even a closed one, is reused;
- a task that is no longer blocked is skipped after one read, and an unknown
  manager is skipped before any search;
- a create held past Dex's async local phase is sent once (the
  duplicate-dispatch test), and so is a comment;
- a create whose response was lost is found in the List and never resent;
- a create answered 503 without being applied completes as `needsReview` and
  is never resent;
- a Worker lost while ClickUp holds the create leaves the next attempt to read
  the List instead of resending;
- an update held past the local phase is dispatched again and converges on one
  assignment;
- a rate-limited search waits for `X-RateLimit-Reset`;
- a rejected create fails the Flow without ClickUp's message text;
- invalid settings fail the Flow before any ClickUp request;
- the example's target, served on the connection's endpoint, starts one Flow
  per signed blocked status, deduplicates a redelivery, rejects a forgery, and
  ignores another status.

Opening a connection from a loaded project needs project storage, so these
tests build the connection with a static credential and run the binding
without its durable project inbox; the Connector SDK's own tests cover the
inbox, including replay after a restart.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 go test -tags=integration ./examples/escalate-blocked-task/... -count=1 -v
```
