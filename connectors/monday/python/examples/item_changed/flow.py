# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per monday.com item change: record it and read the item back."""

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

from dex_connectors.monday import (
    Connection,
    GetItemInput,
    Item,
    ItemChangedEvent,
    ItemChangeKind,
    define_item_changed_trigger_binding,
    new_get_item_step,
)

CONNECTION_NAME = "monday-board-watcher"
ITEM_CHANGED_TRIGGER_BINDING = "board-item-changes"
FLOW_TYPE = "MondayItemChangePython"


@dataclass(frozen=True, slots=True)
class ItemChangeInput:
    """One itemChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: ItemChangedEvent


@dataclass(frozen=True, slots=True)
class ItemChangeOutcome:
    """The Flow result: the changed item and its state read back from monday.com."""

    board_id: str = field(metadata={"dex_json_name": "boardId"})
    item_id: str = field(metadata={"dex_json_name": "itemId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: Item


item_change_attribute = dex.Attribute("monday-item-change", ItemChangeInput)
item_change_outcome_attribute = dex.Attribute("monday-item-change-outcome", ItemChangeOutcome)


def resolve_flow_id(event: TriggerEvent[ItemChangedEvent]) -> str:
    """Name one Flow per change, so a redelivered change finds its existing Flow."""
    return f"monday-item-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[ItemChangedEvent]) -> ItemChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return ItemChangeInput(event.id, event.payload)


def accept_every_item_change(event: TriggerEvent[ItemChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to one group."""
    return True


def map_to_get_item_input(input: ItemChangeInput) -> GetItemInput:
    """Read the changed item by its ID."""
    return GetItemInput(item_id=input.change.item.id)


def validate_item_change_input(input: ItemChangeInput) -> str:
    """Return why a delivered change is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.board_id.strip():
        return "change.boardId is required"
    if not input.change.item.id.strip():
        return "change.item.id is required"
    if input.change.change_kind not in tuple(ItemChangeKind):
        return "change.changeKind must be created or updated"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the item read back beside the delivered change, and complete."
class CompleteItemChange(dex.Step[QueryResult[Item]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteItemChange"

    def execute(self, context: dex.Context, input: QueryResult[Item]) -> dex.StepDecision:
        """Keep the item monday.com returned."""
        change = item_change_attribute.get(context)
        outcome = ItemChangeOutcome(
            change.change.board_id, change.change.item.id, change.change.change_kind, input.value
        )
        item_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Validate and record the delivered item change, then read the item back."
class RecordItemChange(dex.Step[ItemChangeInput]):
    """Validate and record the delivered change."""

    def __init__(self, read_changed_item: dex.StepTarget[ItemChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_changed_item = read_changed_item

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordItemChange"

    def execute(self, context: dex.Context, input: ItemChangeInput) -> dex.StepDecision:
        """Fail an unusable delivery; otherwise record it and read the item back."""
        problem = validate_item_change_input(input)
        if problem:
            return dex.force_fail(problem)
        item_change_attribute.set(context, input)
        return dex.go_to(self.read_changed_item, input)


class ItemChangeFlow(dex.Flow[ItemChangeInput]):
    """Record one monday.com item change and read the item back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static monday.com connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"item change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteItemChange()
        self.read_changed_item = new_get_item_step(
            step_type="ReadChangedItem",
            step_input_type=ItemChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed item back, so the record holds its current state rather "
                "than the poll's copy.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_item_input,
            found=self.complete,
        )
        self.record = RecordItemChange(self.read_changed_item)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "MondayItemChangePython"

    def get_steps(self) -> dex.StepList[ItemChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_item, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(item_change_attribute, item_change_outcome_attribute)

    # Declares the itemChanged binding and its board field for Dex Web; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_item_changed_trigger_binding(
                connection_name="monday-board-watcher",
                binding_name="board-item-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "watchedBoard",
                            "boardIdInput",
                            "Watched board",
                            "Enter the numeric ID of the board whose created and changed items start "
                            "this Flow: open the board at https://monday.com/ and copy the digits after "
                            "/boards/ in its address, such as 1234567890, or paste the whole address. "
                            "The ID is not secret. The connection's user must be able to read the board. "
                            "Blank saves nothing, and the Worker refuses to start without a board. The "
                            "first start records the board's items as a baseline and starts no Flow for "
                            "them. Restart the Worker after saving; a changed board records a new "
                            "baseline.",
                            True,
                            (ConnectorUIBinding("boardId", "/boardId"),),
                        ),
                    )
                ),
            )
        ]
