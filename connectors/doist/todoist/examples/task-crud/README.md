# Todoist CRUD and list example

[TodoistTaskCRUDFlow](flow/workflow.go) has two explicitly different start modes:

- `list` (the default) calls `NewListTasksStep`, persists one task page, and
  completes without any write. Supply the returned cursor in another list run
  to continue with unchanged filters.
- `lifecycle` creates a new disposable task using `NewCreateTaskStep`, reads its
  returned ID with `NewGetTaskStep`, checks that read against the task returned
  by creation, updates selected title/description fields using
  `NewUpdateTaskStep`, reads the task back, locates the changed task through
  `NewListTasksStep`, deletes only that task using `NewDeleteTaskStep`, and
  confirms that `NewGetTaskStep` returns `notFound`.

Every provider call runs inside a connector Step. The created task ID, update
and delete results, request, page counter, and final outcome are registered
Attributes. Retained summary/Display RPCs read persisted state only. The
lifecycle list path follows at most fifty pages. It never chooses mutation
or deletion targets by title and never modifies unrelated listed tasks.

The `updated` branch reads the known task ID with `GetUpdatedLifecycleTask`.
The read compares only requested replacement fields. Matching updates continue
to list and delete. Safe update failures retry the same absolute replacement;
exhausted retries fail the Flow.

Conclusive missing/rejected reads or updates and uncertain creation fail the
Flow through unwired optional branches. A failed lifecycle may leave its created
task. Display exposes created,
update, read-back, and completion Attributes without calling Todoist. A 404
delete result is wired as `alreadyAbsent`, followed by the final lookup.

## Validate and configure

From the connector module, using the latest stable Dex CLI:

```bash
dexcli visualize ./examples/task-crud/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/todoist-crud-rendering/task-crud
```

The FDG must be valid. The local unpublished-module warning is expected. The same directory supplies the local Dex Web Flow rendering.

From the repository root, build
local release metadata to make the connector available in Dex Web:

```bash
mkdir -p /tmp/todoist-release
GOWORK=off go run ./cmd/connectorctl release-artifact \
  --manifest connectors/doist/todoist/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/doist/todoist \
  --version v0.3.0 --tag connectors/doist/todoist/v0.3.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/todoist-release/connector-release.json \
  --digest-output /tmp/todoist-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir "/tmp/todoist-crud-rendering" \
  --connector-config-dir /tmp/todoist-connections \
  --connector-release-override todoist=/tmp/todoist-release
```

Open the printed Dex Web URL and its Connectors/Connections page. Select
`todoist / todoist`, follow the authorization guide, paste the token from
[Todoist's Developer settings](https://app.todoist.com/app/settings/integrations/developer)
into `api_token`, leave non-secret settings blank, and save. Never put a token
in Flow input or source code.

No operation/Trigger configuration UI units are introduced.

The [Worker](main.go) uses `NewProjectConnection` and the project's
[DEX_PROJECT environment](../../../../../sdkgo/projectconfig/README.md#application-loading).
From the connector module:

```bash
GOWORK=off go run ./examples/task-crud
```

Set `DEX_FLOW_SERVICE_ADDRESS` and `DEX_BLOB_CACHE_DIR` as appropriate.
In Dex Web Start Flow, choose `TodoistTaskCRUDFlow` and a unique
Flow ID. For read-only listing, submit:

```json
{"mode":"list","limit":200}
```

For the disposable CRUD lifecycle, submit:

```json
{"mode":"lifecycle","content":"Dex Todoist CRUD local test"}
```

Lifecycle mode creates in Inbox when `projectId` is blank. `description`
sets the original description. `updateContent` and `updateDescription` are
optional replacements: omitted fields remain unchanged and empty
`updateDescription` clears the description. If both are omitted, the example
changes the title to the original content plus ` - updated`. It then deletes
the new task. For a description-only update that preserves the title:

```json
{"mode":"lifecycle","content":"Temporary task","description":"Before","updateDescription":"After"}
```

Project moves are outside the update operation's scope. Successful
completion returns `deletedAndVerified`. This means deletion was acknowledged
or the target was already absent, and the final task lookup returned 404.
Listing returns `listed`, one page of tasks, and `nextCursor` when present.

## Tests

Run the deterministic real-Dex suite against a local Dex Server:

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/task-crud/flow -count=1
```

It verifies read-only listing makes no writes, the complete lifecycle, a
rate-limited or unavailable create that retries with the same provider request identity,
multi-page verification, deletion that applies before a 503 and then returns
404 on retry, a lost create response that remains uncertain, a create answer
with only a task ID that the connector confirms by reading it, invalid input,
description-only updates, clearing a description, updating both fields,
read/update retry, a credential read that fails once and then retries,
applied/unapplied update 503 responses, an unusable update answer whose
read-back finds no active task (`notFound`) or is forbidden
(`providerRejected`), conclusive rejection/missing reads, and malformed reads.

Current local CLI versions may save credentials to a local connection file
while the production Worker uses project storage. The
[test-only live adapter](flow/workflow_live_test.go) reads that user-controlled
file and runs the same Flow through a real Worker and Client:

```bash
DEX_CONNECTOR_CONFIG_FILE=/tmp/todoist-connections/connections.json \
TODOIST_CONNECTOR_ALLOW_LOCAL_TEST=true \
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 \
GOWORK=off go test -tags='integration live' -run TestLiveTaskCRUD -v ./examples/task-crud/flow -count=1
```

This starts three disposable lifecycle runs: description-only, clearing the description, and updating both title and description. Create and
delete now execute through the connector Steps, with no raw API fixture setup
or teardown on the successful path. It checks retained Display data for secret
leakage. It does not force a real provider outage or test request-ID retention;
those limits remain explicit rather than claiming an exactly-once result.
