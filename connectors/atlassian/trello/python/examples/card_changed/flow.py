# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""A Flow started by the Trello cardChanged Trigger that reads each changed card back from Trello."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.atlassian_trello import (
    CARD_ACTION_DELETED,
    WATCHED_CARD_ACTION_TYPES,
    Card,
    CardChangedEvent,
    Connection,
    GetCardInput,
    define_card_changed_trigger_binding,
    new_get_card_step,
)

CONNECTION_NAME = "trello-card-watcher"
CARD_CHANGED_TRIGGER_BINDING = "board-card-changes"
FLOW_TYPE = "TrelloCardChangePython"


@dataclass(frozen=True, slots=True)
class CardChangeInput:
    """One cardChanged delivery and the stable Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: CardChangedEvent


@dataclass(frozen=True, slots=True)
class CardChangeOutcome:
    """The Flow result: the delivered action and the card read back, None for a deleted card."""

    board_id: str = field(metadata={"dex_json_name": "boardId"})
    card_id: str = field(metadata={"dex_json_name": "cardId"})
    action_type: str = field(metadata={"dex_json_name": "actionType"})
    current: Card | None = field(default=None, metadata={"dex_json_omit_default": True})


CARD_CHANGE = dex.Attribute("trello-card-change", CardChangeInput)
CARD_CHANGE_OUTCOME = dex.Attribute("trello-card-change-outcome", CardChangeOutcome)


def resolve_flow_id(event: TriggerEvent[CardChangedEvent]) -> str:
    """Name one Flow per card action; a redelivered action finds its existing Flow."""
    return f"trello-card-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[CardChangedEvent]) -> CardChangeInput:
    """Keep the Trigger event ID beside the delivered card action."""
    return CardChangeInput(trigger_event_id=event.id, change=event.payload)


def accepts_every_card_change(event: TriggerEvent[CardChangedEvent]) -> bool:
    """Admit every card action; an application narrows it, for example to comments."""
    return True


def map_to_get_card_input(input: CardChangeInput) -> GetCardInput:
    """Read the changed card."""
    return GetCardInput(input.change.card.id)


def validate_card_change(input: CardChangeInput) -> str:
    """Return why a delivered card action is unusable, or an empty string."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.action_id.strip() or not input.change.card.id.strip():
        return "change.actionId and change.card.id are required"
    if input.change.action_type not in WATCHED_CARD_ACTION_TYPES:
        return "change.actionType must be a Trello card action type such as createCard or updateCard"
    return ""


def _outcome(input: CardChangeInput, current: Card | None) -> CardChangeOutcome:
    change = input.change
    return CardChangeOutcome(change.board_id, change.card.id, change.action_type, current)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the card action; a deleted card completes, any other is read back."
class RecordCardChange(dex.Step[CardChangeInput]):
    """Validate and record the delivered card action."""

    def get_step_type(self) -> str:
        """Return the stable start Step identity."""
        return "RecordCardChange"

    def execute(self, context: dex.Context, input: CardChangeInput) -> dex.StepDecision:
        """Fail an unusable action; complete a deletion; otherwise read the card."""
        problem = validate_card_change(input)
        if problem:
            return dex.force_fail(problem)
        CARD_CHANGE.set(context, input)
        if input.change.action_type == CARD_ACTION_DELETED:
            outcome = _outcome(input, None)
            CARD_CHANGE_OUTCOME.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef("ReadChangedCard"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the card read back from Trello as the outcome and complete the Flow."
class CompleteCardChange(dex.Step[QueryResult[Card]]):
    """Record the card read back as the outcome."""

    def get_step_type(self) -> str:
        """Return the stable completion Step identity."""
        return "CompleteCardChange"

    def execute(self, context: dex.Context, input: QueryResult[Card]) -> dex.StepDecision:
        """Save the outcome and complete with it."""
        outcome = _outcome(CARD_CHANGE.get(context), input.value)
        CARD_CHANGE_OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record a card deleted after its action without a current state and complete the Flow."
class RecordCardRemovedSince(dex.Step[QueryResult[Card]]):
    """Record a card that was deleted after its action."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordCardRemovedSince"

    def execute(self, context: dex.Context, input: QueryResult[Card]) -> dex.StepDecision:
        """Save the outcome without a current card and complete."""
        outcome = _outcome(CARD_CHANGE.get(context), None)
        CARD_CHANGE_OUTCOME.set(context, outcome)
        return dex.graceful_complete(outcome)


class CardChangeFlow(dex.Flow[CardChangeInput]):
    """Record one Trello card action and read the card back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Trello connection at registration time."""
        if connection is None or connection.reference.name != CONNECTION_NAME:
            raise ValueError("Trello card change Flow requires its static connection name")
        self.record = RecordCardChange()
        self.complete = CompleteCardChange()
        self.removed = RecordCardRemovedSince()
        # dex:group group-id:change group-label:"Change"
        # dex:explanation text:"Read the changed card back, so the record holds its current state."
        self.read = new_get_card_step(
            step_type="ReadChangedCard",
            step_input_type=CardChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed card back, so the record holds its current state.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_card_input,
            found=self.complete,
            # A card deleted after its action still completes, recorded without a current state.
            not_found=self.removed,
        )

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "TrelloCardChangePython"

    def get_steps(self) -> dex.StepList[CardChangeInput]:
        """Register the record, read, and completion Steps."""
        return dex.StepList.start_step(self.record).other_steps(self.read, self.complete, self.removed)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(CARD_CHANGE, CARD_CHANGE_OUTCOME)

    # Declares the cardChanged binding and its board input; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_card_changed_trigger_binding(
                connection_name="trello-card-watcher",
                binding_name="board-card-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedBoard",
                            unit_id="boardInput",
                            label="Watched board",
                            description="Open the board at https://trello.com as the connection's member and paste its address, https://trello.com/b/<short link>/<board name>; the unit saves the 8-character short link, or a 24-character board ID pasted instead. It is not secret. Every card created, updated, commented on, moved in, or deleted on that board starts this Flow. The board is required: leaving it blank stops the Worker at start. The first start records earlier actions as a baseline and starts no Flow for them. Restart the Worker after saving; a changed board records a new baseline.",  # noqa: E501
                            required=True,
                            bindings=(ConnectorUIBinding(port="boardId", json_pointer="/boardId"),),
                        ),
                    )
                ),
            )
        ]
