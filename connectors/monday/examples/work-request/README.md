# monday.com work-request example

This example runs one operation-only Flow from Dex Web **Start Flow** and uses
every monday.com operation to handle a recurring work request:

1. `RecordWorkRequest` validates the board ID, item name, status and due date
   columns and values, and update text, and records the request;
2. `FindExistingItem` calls `monday.NewListItemsStep` with an `any_of` rule on
   the `name` column, an exact name match, for one page of 25 active items
   with the status and due date columns. `ChooseExistingItem` picks the first
   item whose name is exact and whose status is not `Done`. When the page holds
   none, it reads the next page with the cursor, at most five pages, and
   otherwise creates the item;
3. `ReadExistingItem` calls `monday.NewGetItemStep`, and `ConfirmExistingItem`
   schedules the item only when, as just read, it is still active on the board
   with the exact name and an open status. A deleted item's `notFound` is
   wired to the same Step, which then creates the item;
4. `ScheduleExistingItem` calls `monday.NewUpdateItemColumnValuesStep` to set
   the status label and due date;
5. otherwise `CreateWorkItem` calls `monday.NewCreateItemStep` in the requested
   group with the status label and due date;
6. `AddWorkUpdate` calls `monday.NewAddUpdateStep` to add one update saying
   what the Flow did, followed by the request's text, and `CompleteWorkRequest`
   completes with the outcome.

The `uncertain` branches of the create and the update are wired. When
monday.com accepted a write but answered unusably, the Flow completes with
`needsReview: true`, the `reviewReason`, and the connector's `reviewDetail`, so
a person checks the board instead of the Flow writing again. Every other
optional branch is unwired and fails the Flow, such as a rejected column value,
a missing board, an invalid response, or a local defect.

## Generate the Flow Definition

Generate strict FDG 2.0 from `connectors/monday` with the dexcli release pinned
in the repository's `.dex-compat-version` file:

```bash
dexcli visualize ./examples/work-request/flow/workflow.go \
  --schema-version 2.0 --json --out /tmp/monday-work-request
```

The command must report `valid: true`. Inside this repository it also warns
`connector_release_required` for each connector Step, because a local module
is not a published release.

## Configure and run

This example is part of the connector module, so Dex needs release metadata
built from this source, passed as an override; without it the connection
shows **Unsupported**. The connector has no Studio bundle. From the repository
root:

```bash
cd "$(git rev-parse --show-toplevel)"
mkdir -p /tmp/monday-release
go run ./cmd/connectorctl release-artifact \
  --manifest connectors/monday/connector.yaml \
  --module-path github.com/superdurable/dex-connectors-library/connectors/monday \
  --version v0.1.0 --tag connectors/monday/v0.1.0 \
  --source-sha "$(git rev-parse HEAD)" \
  --output /tmp/monday-release/connector-release.json \
  --digest-output /tmp/monday-release/connector-release.json.sha256
dexcli dev \
  --flow-rendering-dir /tmp \
  --connector-config-dir "$HOME/.dex/connectors" \
  --connector-release-override monday=/tmp/monday-release
```

Follow the [monday.com setup](../../README.md#mondaycom-setup), then open
**Connections** in Dex Web and select `monday / monday-workspace`, which all
five monday.com Steps of `MondayWorkRequest` use. Choose **Personal API
token** and paste the token, or **monday.com OAuth 2.1** and authorize, and
save; the status becomes **Ready**. The example has no Step configuration.
The token is stored only in the plaintext development file shown on the page;
never commit or share it.

In a second terminal, start the Worker from `connectors/monday` with that file:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/work-request
```

The Worker reads the connection at startup and the credentials before every
call. The default Worker address is `127.0.0.1:8834`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed. For local verification against a monday.com-compatible fake only,
`MONDAY_LOCAL_API_URL` replaces `https://api.monday.com/v2`. It must be HTTPS
or a loopback HTTP URL, and production Workers leave it unset.

In the Run workspace, choose **Start Flow**, select `MondayWorkRequest`, choose
the Worker at `127.0.0.1:8834`, enter a unique Flow ID, and submit. The board
ID is the number after `/boards/` in the board's URL. Column IDs appear in the
board with monday.com's **Developer mode**, under the profile picture >
**monday.labs**. `statusLabel` must already exist in the status column.

```json
{
  "boardId": "1234567890",
  "groupId": "topics",
  "itemName": "Monthly Fire Drill Checklist - February",
  "statusColumnId": "status",
  "statusLabel": "Working on it",
  "dueDateColumnId": "date4",
  "dueDate": "2026-02-18",
  "updateText": "Scheduled from the facilities request."
}
```

Omit `groupId` to create a new item in the board's first group. The Flow
result and the `monday-work-outcome` Attribute hold the action (`created`,
`scheduled`, or `creationUncertain`), the item ID and URL, the update ID, and
the review state. The run writes to the real board; delete the test item in
monday.com afterwards.

## Test

```bash
GOWORK=off go test -race ./examples/work-request/...
```

With the pinned Dex development server running, the integration tests drive
the Flow on a real Worker against a stateful fake monday.com. Like monday.com,
the fake caches each `Idempotency-Key`'s response, answers a concurrent
duplicate with `409 IDEMPOTENCY_CONFLICT`, and replays a finished one with
`Idempotency-Replayed: true`:

- a new request creates one item with its status and due date and adds one
  update;
- an open same-name item in the backlog is scheduled, and these decoys stay
  untouched: a completed same-name item, an archived one, one on another
  board, and a look-alike name;
- a first page of completed same-name items leads to the cursor's second page;
- a create, a column update, and an update held for nine seconds, past Dex's
  async local phase, are each dispatched again. The repeat gets 409 and then
  the replay, so monday.com runs each request once and the board holds one
  item and one update;
- a lost create response is retried and replayed, creating one item;
- a Worker lost while monday.com holds the create is replaced, and the new
  Worker's attempt sends the same key and receives the first item;
- a complexity-limited create waits for `retry_in_seconds` and creates one
  item;
- an unusable create answer completes as `needsReview` and is never resent;
- a rejected column value fails the Flow through the unwired
  `providerRejected` branch without monday.com's message text;
- an invalid due date fails the Flow before any monday.com request.

```bash
GOWORK=off go test -tags=integration ./examples/work-request/... -count=1 -v
```
