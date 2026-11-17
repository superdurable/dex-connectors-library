# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Record each verified ClickUp task webhook in a Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.clickup import Connection, TaskEvent, define_task_event_trigger_binding

CONNECTION_NAME = "clickup-workspace"
BINDING_NAME = "task-events"
FLOW_ID_PREFIX = "clickup-task-event-"


@dataclass(frozen=True, slots=True)
class TaskEventInput:
    """The verified ClickUp event delivered to one Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    received: TaskEvent


task_event_attribute = dex.Attribute("clickup-task-event", TaskEventInput)


def resolve_flow_id(event: TriggerEvent[TaskEvent]) -> str:
    """Use the provider history ID so ClickUp redelivery finds the existing Flow."""
    return FLOW_ID_PREFIX + event.id.replace(":", "-")


def map_to_flow_input(event: TriggerEvent[TaskEvent]) -> TaskEventInput:
    """Retain the delivery ID beside the event payload."""
    return TaskEventInput(event.id, event.payload)


# dex:group group-id:task-event group-label:"Task event"
# dex:explanation text:"Record the verified task event and complete its Flow."
class RecordTaskEvent(dex.Step[TaskEventInput]):
    """Persist the delivered task event and complete the Flow."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordTaskEvent"

    def execute(self, context: dex.Context, input: TaskEventInput) -> dex.StepDecision:
        """Fail incomplete input; otherwise store the event and finish."""
        if not input.trigger_event_id or not input.received.task_id:
            return dex.force_fail("triggerEventId and received.taskId are required")
        task_event_attribute.set(context, input)
        return dex.graceful_complete(input)


class TaskEventFlow(dex.Flow[TaskEventInput]):
    """One Flow per verified ClickUp task event."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static ClickUp connection."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task event Flow requires the static ClickUp connection")
        self.record = RecordTaskEvent()

    def get_flow_type(self) -> str:
        """Keep the Flow identity stable."""
        return "ClickUpTaskEventPython"

    def get_steps(self) -> dex.StepList[TaskEventInput]:
        """Record the event in one Step."""
        return dex.StepList.start_step(self.record)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the task event Attribute."""
        return dex.PersistenceSchema.of(task_event_attribute)

    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_task_event_trigger_binding(
                connection_name="clickup-workspace",
                binding_name="task-events",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "events",
                            "taskEventPicker",
                            "Task events",
                            "Select the task events this Flow records. The picker stores event names; "
                            "leave it empty to record every task event subscribed by the webhook.",
                            False,
                            (ConnectorUIBinding("events", "/events"),),
                        ),
                    )
                ),
            )
        ]
