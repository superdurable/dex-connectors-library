# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Every task added to, changed in, removed from, deleted from, or restored to the picked project starts one
Flow, which reads the task back and records its current state."""

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

from dex_connectors.asana import (
    Connection,
    GetTaskInput,
    Task,
    TaskChangeAction,
    TaskChangedEvent,
    define_task_changed_trigger_binding,
    new_get_task_step,
)

CONNECTION_NAME = "asana-task-watcher"
TASK_CHANGED_TRIGGER_BINDING = "project-task-changes"
FLOW_ID_PREFIX = "asana-task-change-"


@dataclass(frozen=True, slots=True)
class TaskChangeInput:
    """One taskChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: TaskChangedEvent


@dataclass(frozen=True, slots=True)
class ChangeOutcome:
    """The Flow result and the value of its outcome Attribute."""

    project_id: str = field(metadata={"dex_json_name": "projectId"})
    task_id: str = field(metadata={"dex_json_name": "taskId"})
    action: str
    changed_field: str = field(
        default="", metadata={"dex_json_name": "changedField", "dex_json_omit_default": True}
    )
    current: Task | None = field(default=None, metadata={"dex_json_omit_default": True})
    """The task read back from Asana; None for a deleted task, which is not read."""


task_change_attribute = dex.Attribute("asana-task-change", TaskChangeInput)
task_change_outcome_attribute = dex.Attribute("asana-task-change-outcome", ChangeOutcome)


def resolve_flow_id(event: TriggerEvent[TaskChangedEvent]) -> str:
    """Name one Flow per task event; a redelivered event finds its existing Flow."""
    return FLOW_ID_PREFIX + event.id


def map_to_flow_input(event: TriggerEvent[TaskChangedEvent]) -> TaskChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return TaskChangeInput(trigger_event_id=event.id, change=event.payload)


def accept_every_task_change(event: TriggerEvent[TaskChangedEvent]) -> bool:
    """Admit every task event; an application narrows it, for example to completions."""
    return True


def map_to_get_task_input(input: TaskChangeInput) -> GetTaskInput:
    """Read the changed task."""
    return GetTaskInput(task_id=input.change.task.id)


def new_change_outcome(change: TaskChangedEvent, current: Task | None) -> ChangeOutcome:
    """Summarize a change and the task read back for it."""
    return ChangeOutcome(change.project_id, change.task.id, change.action, change.changed_field, current)


def validate_task_change_input(input: TaskChangeInput) -> str:
    """Return why the delivered change cannot be recorded, or an empty string."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.project_id.strip() or not input.change.task.id.strip():
        return "change.projectId and change.task.id are required"
    if input.change.action not in {action.value for action in TaskChangeAction}:
        return "change.action must be added, changed, removed, deleted, or undeleted"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the task event; complete a deleted task at once, otherwise read the task back."
class RecordTaskChange(dex.Step[TaskChangeInput]):
    """Store the delivered change and decide whether Asana has a task to read."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "RecordTaskChange"

    def execute(self, context: dex.Context, input: TaskChangeInput) -> dex.StepDecision:
        """Fail an invalid delivery, complete a deletion, and read any other changed task."""
        problem = validate_task_change_input(input)
        if problem:
            return dex.force_fail(problem)
        task_change_attribute.set(context, input)
        if input.change.action == TaskChangeAction.DELETED:
            outcome = new_change_outcome(input.change, None)
            task_change_outcome_attribute.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(dex.StepRef("ReadChangedTask"), input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the task read back from Asana as the outcome and complete the Flow."
class CompleteTaskChange(dex.Step[QueryResult[Task]]):
    """Complete with the change and the task's current state."""

    def get_step_type(self) -> str:
        """Return this Step's stable type."""
        return "CompleteTaskChange"

    def execute(self, context: dex.Context, input: QueryResult[Task]) -> dex.StepDecision:
        """Save and return the outcome."""
        change = task_change_attribute.get(context)
        outcome = new_change_outcome(change.change, input.value)
        task_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class TaskChangedFlow(dex.Flow[TaskChangeInput]):
    """Record one Asana task change, reading the task back unless it was deleted."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Asana connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("task change Flow requires the asana-task-watcher connection")
        self.record = RecordTaskChange()
        self.complete = CompleteTaskChange()
        self.read_task = new_get_task_step(
            step_type="ReadChangedTask",
            step_input_type=TaskChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed task back, because Asana events name the change but not "
                "the task's current state.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_task_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Dex Flow identity."""
        return "AsanaTaskChangePython"

    def get_steps(self) -> dex.StepList[TaskChangeInput]:
        """Record the change, read the task back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read_task, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(task_change_attribute, task_change_outcome_attribute)

    # Declares the taskChanged binding and its project picker for Dex Web; Dex reads one static return.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_task_changed_trigger_binding(
                connection_name="asana-task-watcher",
                binding_name="project-task-changes",
                configuration_ui=ConnectorConfigurationUI(
                    (
                        ConnectorUIUnit(
                            "watchedProject",
                            "projectPicker",
                            "Watched project",
                            "Choose the workspace, then the Asana project whose tasks start this Flow when "
                            "they are added, changed, removed, deleted, or restored, subtasks included; the "
                            "lists come from this connection's Asana user and save the workspace ID, project"
                            " ID, and project name. Without the lists, enter the project gid, the long "
                            "number after /project/ in the project's Asana web address. A project is "
                            "required; there is no blank default. The first start records a baseline and "
                            "starts no Flow for existing tasks. Restart the Worker after saving; a changed "
                            "project records a new baseline.",
                            True,
                            (
                                ConnectorUIBinding("workspaceId", "/workspaceId"),
                                ConnectorUIBinding("projectId", "/projectId"),
                                ConnectorUIBinding("projectName", "/projectName"),
                            ),
                        ),
                    )
                ),
            )
        ]
