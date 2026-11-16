# Trello card-changed example (TypeScript)

This example starts one `TrelloCardChangeTypeScript` Flow for every card action
on one Trello board, through the `cardChanged` poll Trigger, at parity with the
[Go example](../../../go/examples/card-changed/README.md):

1. `RecordCardChange` validates the delivered action and records it. A
   `deleteCard` action completes at once without reading the card.
2. `ReadChangedCard` calls `newGetCardStep`, so the record holds the card as
   Trello returns it now.
3. `CompleteCardChange` records that card and completes the Flow. A card
   deleted after its action selects `notFound`, which the Flow wires to
   `RecordCardRemovedSince` and completes without a current card.

The Flow ID is `trello-card-change-<Trigger event ID>`, and the event ID is
`trello-action-<Trello action ID>`, so a redelivered action finds its existing
Flow. The first start records the board's earlier actions as a baseline and
starts no Flow for them.

## Configure

Follow the [Trello connector setup](../../../README.md), then save the API key
and token for the `trello-card-watcher` connection under **Connections** in Dex
Web. **Poll interval** is the delay between polls, at least 10 seconds; each
poll normally makes one request, which spends the token's Trello rate limit of
100 requests per 10 seconds. Under the `board-card-changes` binding of
`cardChanged`, paste the board's address into **Watched board**: open the board
at https://trello.com and copy `https://trello.com/b/<short link>/<board name>`.
The unit saves the 8-character short link; a 24-character board ID works too.
The board is required, so an unsaved binding stops the Worker at start. The
Worker reads saved values at startup, so restart it after saving; a changed
board records a new baseline.

## Run

From `connectors/atlassian/trello/typescript`, generate and lint strict FDG 2.0:

```sh
npm run build
npm install --prefix examples/card-changed --no-save --no-package-lock --ignore-scripts .
cd examples/card-changed
dexcli visualize flow/workflow.ts --language typescript --schema-version 2.0 --lint app --json --out ../../build/card-changed --open=false
```

Start `dexcli dev -flow-rendering-dir ./build/card-changed`, then run the
Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json npm run example:changes
```

[`app.ts`](app.ts) wires the binding to the Flow:

```ts
const runner = newProjectCardTriggerRunner(project, ConnectionName, [{
  bindingName: CardChangedTriggerBinding, target: newDexFlowTriggerTarget(client, flow, acceptEveryCardChange, resolveFlowId, mapToFlowInput, { logger: options.logger }),
}], trelloOptions);
```

With hosted project storage, each action is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats an action. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline and actions made while it
was stopped are not delivered. Set `LOG_LEVEL=debug` to see every committed
poll and delivery.

## Test

`test/card_changed_workflow.test.ts` covers the Flow's mapping, validation,
binding declaration, and project wiring. With `dexcli dev` running, the
integration tests run this application against a real Dex Worker and Client
and a local stand-in for the board's action history and cards: the baseline
starts no Flow, each card action starts one Flow across a restart (a card
deleted after its action completes without a current card), a 429 pauses
polling without losing an action, and a changed board records a new baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 npm run test:integration
```

Live Trello polling has not been verified.
