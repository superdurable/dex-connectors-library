# Asana approved request example

This example runs one operation-only Flow from Dex Web **Start Flow**. It turns
an approved request into an Asana task, assigns it, and comments with the
approval, using every Asana operation:

1. `RecordApprovedRequest` validates the request and records it with the picked
   project and section. The task name is the request ID in brackets, then the
   title, such as `[REQ-1042] Replace badge reader`;
2. `FindOpenRequestTasks` calls `asana.NewListTasksStep` for one page of the
   project's incomplete tasks, and `EvaluateOpenRequestTasks` reuses a task
   whose name starts with `[REQ-1042]`. `[REQ-10420] ...`, a name that only
   mentions `REQ-1042`, and a completed task are not reused. The Flow follows
   Asana's offset through at most five pages of 100 tasks and completes as
   `duplicateCheckIncomplete` when the project has more. This is a business
   duplicate check, never the retry-safety mechanism;
3. `CreateRequestTask` calls `asana.NewCreateTaskStep` with sync durability,
   creating the task in the picked section with its notes and due date.
   `created` records the task gid, `providerRejected` completes as `rejected`,
   and `uncertain` parks the Flow for an operator;
4. `AssignRequestTask` calls `asana.NewUpdateTaskStep` to assign the task, set
   its due date, and move a reused task into the picked section; the update is
   repeatable, so it keeps async durability. `providerRejected`, such as an
   assignee outside the workspace, completes as `assignmentRejected`;
5. `CommentOnRequestTask` calls `asana.NewAddCommentStep` with
   `Approved by <approver>.` and the approval note. Both `added` and `uncertain`
   complete as `ready`; an uncertain comment is recorded and never re-sent.

Unwired optional branches, such as a rejected list or a missing task during
assignment, fail the Flow.

## Reconciling an uncertain create

`uncertain` means the task may exist. The Flow records the connector Call ID and
the time the outcome was observed, sets the phase to `needsReconciliation`, and
waits. An operator looks in the project for a task named with the request ID
around that time, then runs one of two Actions, each requiring
`asana-request-task.reconcile`:

- **Confirm created task** takes the task gid the operator found. The Flow reads
  it with `getTask` and adopts it only when it is in the project and its name
  starts with the request ID; otherwise it returns to the operator with a note.
- **Create task again** is the only path that creates again, as a new Step
  execution with a new connector Call ID.

## Configure

Follow the [Asana connector setup](../../README.md), then save a personal access
token for the `asana-requests` connection under **Connections** in Dex Web.
The **Request project and section** picker on the `CreateRequestTask` Step
saves the workspace, project, and optional section; the duplicate check and
assignment use the same project and section. Leave it unsaved to use each
Start Flow input's `projectId` and `sectionId`. The Worker reads the saved value
once at startup, so restart it after saving.

## Run

Generate strict FDG 2.0 from `connectors/asana` with the latest stable dexcli
release:

```bash
mkdir -p build
dexcli visualize ./examples/approved-request-task/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/approved-request-task
```

Run `dexcli dev` with that build directory, then run the Worker with the
connection path shown by Dex Web:

```bash
export DEX_CONNECTOR_CONFIG_FILE="$HOME/.dex/connectors/connections.json"
go run ./examples/approved-request-task
```

The default Worker address is `127.0.0.1:8831`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `AsanaApprovedRequestTask` with a unique Flow ID:

```json
{
  "requestId": "REQ-1042",
  "title": "Replace badge reader",
  "details": "Badge reader at door 4 is offline.",
  "approvedBy": "Grace Hopper",
  "approvalNote": "Budget code FAC-7.",
  "assigneeId": "ada@example.com",
  "dueOn": "2026-10-15",
  "projectId": "1201000000000001",
  "sectionId": "1201000000000101"
}
```

`assigneeId` is `me`, a user gid, or a workspace member's email address. The
Flow result and the `asana-request-task` Attribute hold the phase, the task gid,
the task read back after assignment, and the comment's story gid.

## Test

```bash
GOWORK=off go test -race ./examples/approved-request-task/...
```

With the latest Dex development server running, the integration test drives the
Flow on a real Worker against a stateful fake Asana: a new task created once on
the second page beside a near-duplicate, a mention, and a completed task, then
assigned, moved, and commented; an open task with the request ID reused and
moved out of intake; a rejected create; a rate-limited create retried after
`Retry-After`; a create timeout reconciled by confirming a missing, a
mismatched, and then the real task gid without a second create; a 5xx create
created again only after operator approval; a nine-second create and a
nine-second comment each sent once under sync durability; a nine-second update
whose backup attempt resends the same move and `PUT` and leaves the same task; a
comment timeout recorded without a resend; and an assignee Asana rejects.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/approved-request-task/... -count=1 -v
```
