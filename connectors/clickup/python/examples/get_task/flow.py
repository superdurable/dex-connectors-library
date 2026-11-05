# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read one ClickUp task through a typed Dex Flow."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.clickup import Connection, GetTaskInput, Task, new_get_task_step

CONNECTION_NAME = "clickup-workspace"
FLOW_TYPE = "ClickUpGetTaskPython"
TASK_RESULT = dex.Attribute("clickup-get-task-result", Task)


@dataclass(frozen=True, slots=True)
class GetTaskRequest:
    """Task ID selected for one Flow run."""

    task_id: str = field(metadata={"dex_json_name": "taskId"})

    def to_operation_input(self) -> GetTaskInput:
        """Name the ClickUp task to read."""
        return GetTaskInput(task_id=self.task_id)


# dex:group group-id:clickup-task group-label:"ClickUp task"
# dex:explanation text:"Store the task returned by ClickUp and complete the Flow."
class CompleteTaskRead(dex.Step[QueryResult[Task]]):
    """Persist the provider task result."""

    def execute(self, context: dex.Context, input: QueryResult[Task]) -> dex.StepDecision:
        """Complete after saving the task for external reads."""
        TASK_RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class GetTaskFlow(dex.Flow[GetTaskRequest]):
    """Read one task through the static ClickUp connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("getTask requires the static ClickUp connection")
        self.complete = CompleteTaskRead()
        self.read = new_get_task_step(
            step_type="ReadClickUpTask",
            step_input_type=GetTaskRequest,
            annotations={
                "groupId": "clickup-task",
                "groupLabel": "ClickUp task",
                "explanation": "Read one ClickUp task with its custom fields.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=GetTaskRequest.to_operation_input,
            found=self.complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[GetTaskRequest]:
        """Register the provider Step and completion Step."""
        return dex.StepList.start_step(self.read).other_steps(self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the retained task Attribute."""
        return dex.PersistenceSchema.of(TASK_RESULT)
