# Trello card-changed Flow for Python

This example starts one `TrelloCardChangePython` Flow for every card action on
one Trello board, through the `cardChanged` poll Trigger, at parity with the
[Go example](../../../go/examples/card-changed/README.md):

1. `RecordCardChange` validates the delivered action and records it. A
   `deleteCard` action completes at once without reading the card.
2. `ReadChangedCard` calls `new_get_card_step`, so the record holds the card as
   Trello returns it now.
3. `CompleteCardChange` records that card and completes the Flow. A card
   deleted after its action selects `not_found`, which the Flow wires to
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

From `connectors/atlassian/trello/python`, generate strict FDG 2.0 and start
`dexcli dev` with it:

```sh
uv sync --locked
uv run dexcli visualize ./examples/card_changed/flow.py \
  --schema-version 2.0 --json --out ./build/card-changed
dexcli dev -flow-rendering-dir ./build/card-changed
```

Run the Worker and the Trigger with the connections file Dex Web wrote:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 DEX_CONNECTOR_CONFIG_FILE=/path/to/connections.json \
  uv run python -m examples.card_changed.main
```

[`trigger.py`](trigger.py) wires the binding to the Flow:

```python
target = new_dex_flow_trigger_target(
    dex_client,
    flow,
    accepts_event=accepts_every_card_change,
    resolve_flow_id=resolve_flow_id,
    map_to_flow_input=map_to_flow_input,
    logger=logger,
)
return await new_project_card_trigger_runner(
    project,
    CONNECTION_NAME,
    [ProjectCardChangedTriggerRoute(CARD_CHANGED_TRIGGER_BINDING, target)],
    logger=logger,
)
```

With hosted project storage, each action is recorded in the binding's Trigger
inbox before the poll cursor commits, so a restarted Worker neither misses nor
repeats an action. Under `dexcli dev`, the poll cursor stays in process memory,
so a restarted local Worker records a new baseline and actions made while it
was stopped are not delivered. Set `LOG_LEVEL` (`DEBUG`, `INFO`, `WARNING`) for
the `dex_connectors.sdk.trigger` records.

## Test

`tests/test_card_changed_example.py` covers the Flow's mapping, validation, and
binding declaration. With `dexcli dev` running, the integration tests run a
real Dex Worker and Client against a local stand-in for the board's action
history and cards: the baseline starts no Flow, each card action starts one
Flow across a Trigger restart (a card deleted after its action completes
without a current card), a 429 pauses polling without losing an action, and a
changed board records a new baseline:

```sh
DEX_FLOW_SERVICE_ADDRESS=127.0.0.1:8801 uv run pytest examples/card_changed/integration_test.py
```

Live Trello polling has not been verified.
