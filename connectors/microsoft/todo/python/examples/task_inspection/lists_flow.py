# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Microsoft To Do lists inspection Flow."""

import dex
from dex_connectors.sdk import QueryResult

from dex_connectors.microsoft_todo import (
    Connection,
    ListTaskListsInput,
    TaskListsPage,
    new_list_task_lists_step,
)

FLOW_TYPE = "MicrosoftTodoListsInspectionPython"
CONNECTION_NAME = "microsoft-todo"
_CALL_STEP_TYPE = "CallMicrosoftTodoLists"
_RESULT = dex.Attribute("microsoft-todo-lists", TaskListsPage)


def map_to_operation_input(request: ListTaskListsInput) -> ListTaskListsInput:
    """Send the request unchanged."""
    return request


# dex:group group-id:provider group-label:"Microsoft To Do"
# dex:explanation text:"Start the Microsoft To Do request."
class _StartRequest(dex.Step[ListTaskListsInput]):
    def get_step_type(self) -> str:
        return "StartMicrosoftTodoLists"

    def execute(self, context: dex.Context, input: ListTaskListsInput) -> dex.StepDecision:
        return dex.go_to(dex.StepRef("CallMicrosoftTodoLists"), input)


# dex:group group-id:provider group-label:"Microsoft To Do"
# dex:explanation text:"Record the provider result and complete the Flow."
class _CompleteRequest(dex.Step[QueryResult[TaskListsPage]]):
    def get_step_type(self) -> str:
        return "CompleteMicrosoftTodoLists"

    def execute(self, context: dex.Context, input: QueryResult[TaskListsPage]) -> dex.StepDecision:
        _RESULT.set(context, input.value)
        return dex.graceful_complete(input.value)


class TaskListsInspectionFlow(dex.Flow[ListTaskListsInput]):
    """Read lists through the static microsoft-todo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("lists inspection requires the static microsoft-todo connection")
        self._start = _StartRequest()
        self._complete = _CompleteRequest()
        self._call = new_list_task_lists_step(
            step_type=_CALL_STEP_TYPE,
            step_input_type=ListTaskListsInput,
            annotations={
                "groupId": "provider",
                "groupLabel": "Microsoft To Do",
                "explanation": "Read lists from the connected Microsoft To Do account.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_operation_input,
            listed=self._complete,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "MicrosoftTodoListsInspectionPython"

    def get_steps(self) -> dex.StepList[ListTaskListsInput]:
        """Register the start, provider read, and completion Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._call, self._complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist the provider result."""
        return dex.PersistenceSchema.of(_RESULT)
