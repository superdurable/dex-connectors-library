# Trello card change example

This example starts one Flow for every card action on one Trello board, read
through the `cardChanged` poll Trigger:

1. The Worker polls the board's action history every `pollInterval`. The first
   start records the existing actions as a baseline and starts no Flow for them.
2. Each card created, copied, converted from a checklist item, emailed in,
   updated (moved, renamed, archived, rescheduled, and other field changes),
   commented on, moved in from another board, or deleted starts one
   `TrelloCardChange` Flow named after the Trello action ID, so a redelivered
   action finds its existing Flow.
3. `RecordCardChange` records the action. A `deleteCard` action completes at
   once without reading the card.
4. `ReadChangedCard` calls `trello.NewGetCardStep`, and `CompleteCardChange`
   records the card as Trello returns it now. A card deleted after its action
   selects `notFound`, which the Flow wires to `RecordCardRemovedSince` and
   completes without a current card.

## Configure

Follow the [Trello connector setup](../../../README.md), then save the API key
and token for the `trello-card-watcher` connection under **Connections** in
Dex Web. Under the Flow's Trigger binding `board-card-changes`, paste the
board's address into **Watched board**: open the board at https://trello.com
and copy `https://trello.com/b/<short link>/<board name>`. The unit saves the
8-character short link; a 24-character board ID works too. The board is
required, so an unsaved binding stops the Worker at start. Restart the Worker
after saving; a changed board records a new baseline.

## Run

Generate strict FDG 2.0 from `connectors/atlassian/trello/go` with the latest
stable dexcli release:

```bash
mkdir -p build
dexcli visualize ./examples/card-changed/flow/workflow.go \
  --schema-version 2.0 --json --out ./build/card-changed
```

Run `dexcli dev` with that build directory, then run the Worker. It reads the
`DEX_PROJECT_*` project configuration environment described in
[project configuration](../../../../../../sdkgo/projectconfig/README.md); Dex Web
or Superverse Studio writes that configuration when you save the connection
and the binding. Each action is recorded in the binding's project Trigger inbox
before the poll cursor moves past it, so a restarted Worker neither misses nor
repeats one.

```bash
go run ./examples/card-changed
```

Override `DEX_FLOW_SERVICE_ADDRESS`, `DEX_BLOB_CACHE_DIR`, or `LOG_LEVEL`
(`debug`, `info`, `warn`, or `error`) when needed. The Flow result and the
`trello-card-change-outcome` Attribute hold the board, card, action type, and
the card read back from Trello.

## Test

```bash
GOWORK=off go test -race ./examples/card-changed/...
```

With the latest Dex development server running, the integration test drives
the Trigger and the Flow on a real Worker against a fake Trello board that
requires the `OAuth` header. Existing actions are a baseline; a created, moved,
and commented card and a deleted baseline card start four Flows; after a
restart whose first poll Trello answers with 429, a card created while the
Worker was stopped and a card created and deleted while stopped arrive, the
second completing without a current card; no action is delivered twice. A
second test answers the stored cursor's `since` with 400, records a new
baseline without delivering the gap, and then delivers the next card.

```bash
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 GOWORK=off go test -tags=integration ./examples/card-changed/... -count=1 -v
```
