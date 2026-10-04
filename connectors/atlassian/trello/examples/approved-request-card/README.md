# Trello approved request example

This example runs one operation-only Flow from Dex Web **Start Flow**. It turns
an approved request into a Trello card in the approved list and comments with
the approval, using every Trello operation. In Trello a card's list is its
status, so a request card that already exists is moved, not created again:

1. `RecordApprovedRequest` validates the request and records it. The card name
   is the request ID in brackets, then the title, such as
   `[REQ-1042] Replace badge reader`;
2. `FindOpenRequestCards` calls `trello.NewListCardsStep` for one page of the
   board's open cards, newest first, and `EvaluateOpenRequestCards` reuses a
   card whose name starts with `[REQ-1042]`. `[REQ-10420] ...`, a name that only
   mentions `REQ-1042`, and an archived card are not reused. The Flow follows
   `nextBefore` through at most five pages of 100 cards and completes as
   `duplicateCheckIncomplete` when the board has more. This is a business
   duplicate check, never the retry-safety mechanism;
3. `CreateRequestCard` calls `trello.NewCreateCardStep` with sync durability,
   creating the card at the top of the approved list with its description, due
   date, labels, and members. `created` records the card ID,
   `providerRejected` completes as `rejected`, and `uncertain` parks the Flow
   for an operator;
4. `MoveRequestCard` calls `trello.NewUpdateCardStep` for a reused or
   reconciled card: it reopens the card, moves it to the approved list, sets the
   due date, and adds the request labels without removing the card's own. The
   update is repeatable, so it keeps async durability. `providerRejected`, such
   as an unknown label, completes as `moveRejected`;
5. `CommentOnRequestCard` calls `trello.NewAddCommentStep` with
   `Approved by <approver>.` and the approval note. Both `added` and
   `uncertain` complete as `ready`; an uncertain comment is recorded and never
   re-sent.

Unwired optional branches, such as a missing board or an invalid card page,
fail the Flow.

## Reconciling an uncertain create

`uncertain` means the card may exist. The Flow records the connector Call ID,
the time the outcome was observed, and the connector's safe failure, sets the
phase to `needsReconciliation`, and waits. An operator looks on the board for a
card named with the request ID around that time, then runs one of two Actions,
each requiring `trello-request-card.reconcile`:

- **Confirm created card** takes the 24-character card ID the operator found.
  The Flow reads it with `getCard` and adopts it only when it is on the board
  and its name starts with the request ID; it then moves the card to the
  approved list and comments. Otherwise it returns to the operator with a note.
- **Create card again** is the only path that creates again, as a new Step
  execution with a new connector Call ID.

## Configure

Follow the [Trello connector setup](../../README.md), then save the API key and
token for the `trello-requests` connection under **Connections** in Dex Web.
The Flow has no Studio configuration units: Trello's board and list listings
are JSON arrays, which Dex Web's Studio commands reject, so the board and the
approved list are Start Flow input. The connector README shows how to find
their 24-character IDs.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/trello` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/approved-request-card/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/approved-request-card
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../../sdkgo/projectconfig/README.md); Dex Web or
Superverse Studio writes that configuration when you save the connection.

```bash
go run ./examples/approved-request-card
```

The default Worker address is `127.0.0.1:8832`. Override
`DEX_FLOW_SERVICE_ADDRESS`, `DEX_WORKER_BIND_ADDRESS`, or `DEX_BLOB_CACHE_DIR`
when needed.

Start `TrelloApprovedRequestCard` with a unique Flow ID:

```json
{
  "requestId": "REQ-1042",
  "title": "Replace badge reader",
  "details": "Badge reader at door 4 is offline.",
  "approvedBy": "Grace Hopper",
  "approvalNote": "Budget code FAC-7.",
  "boardId": "5b6893f01cb3228998cf629e",
  "listId": "5b6893f01cb3228998cf62a1",
  "dueAt": "2026-10-15T17:00:00Z",
  "labelIds": ["5b6893f01cb3228998cf62b4"],
  "memberIds": ["5a1e4c7b2f0d3e6a9b8c7d6e"]
}
```

`listId` is the approved list on that board. `labelIds` and `memberIds` are
board label and member IDs; members are assigned only to a new card. The Flow
result and the `trello-request-card` Attribute hold the phase, the card ID and
URL, the card read back after a move, and the comment's action ID.

## Test

```bash
GOWORK=off go test -race ./examples/approved-request-card/...
```

With the latest Dex development server running, the integration test drives the
Flow on a real Worker against a stateful fake Trello that requires the `OAuth`
header: a new card created once after a second page beside a near-duplicate, a
mention, and an archived card, then commented; an open card with the request ID
moved to the approved list and labeled without losing its own label; a full
duplicate check that stops without creating; a rejected create; a rate-limited
create retried after `Retry-After`; a create timeout reconciled by confirming a
missing, a mismatched, and then the real card ID without a second create; a 5xx
create created again only after operator approval; a nine-second create and a
nine-second comment each sent once under sync durability; a nine-second move
whose backup attempt resends the same body and leaves the same card; a comment
timeout recorded without a resend; a Worker lost during the create, whose
replacement finds the heartbeat checkpoint and sends nothing; and a move
Trello rejects.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/approved-request-card/... -count=1 -v
```
