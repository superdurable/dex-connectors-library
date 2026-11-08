# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Microsoft To Do tasks inspection Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_todo import (
    Connection,
    ListTasksInput,
    TasksPage,
    new_list_tasks_step,
)

FLOW_TYPE = "MicrosoftTodoTasksInspectionPython"
CONNECTION_NAME = "microsoft-todo"
_CALL_STEP_TYPE = "CallMicrosoftTodoTasks"
_RESULT = dex.Attribute("microsoft-todo-tasks", TasksPage)


def map_to_operation_input(request: ListTasksInput) -> ListTasksInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:provider group-label:"Microsoft To Do"
# dex:explanation text:"Start the Microsoft To Do request."
class _StartRequest(dex.Step[ListTasksInput]):
    def get_step_type(self) -> str:
        return "StartMicrosoftTodoTasks"

    def execute(self, context: dex.Context, input: ListTasksInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMicrosoftTodoTasks"), input)


# dex:group group-id:provider group-label:"Microsoft To Do"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TasksPage]]):
    def get_step_type(self) -> str:
        return "CompleteMicrosoftTodoTasks"

    def execute(self, context: dex.Context, input: QueryResult[TasksPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TasksInspectionFlow(dex.Flow[ListTasksInput]):
    """Read tasks through the static microsoft-todo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("tasks inspection requires the static microsoft-todo connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_tasks_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListTasksInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Microsoft To Do",
                "explanation": "Read tasks from the connected Microsoft To Do account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MicrosoftTodoTasksInspectionPython"

    def get_steps(self) -> dex.StepList[ListTasksInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
