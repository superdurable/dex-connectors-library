# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import ListTaskListsInput, ListTasksInput, TaskListsPage, TasksPage

StepInput = TypeVar("StepInput")

def new_list_task_lists_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListTaskListsInput],
    listed: dex.StepTarget[QueryResult[TaskListsPage]],
    provider_rejected: dex.StepTarget[QueryResult[TaskListsPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[TaskListsPage]] | None = None,
    defect: dex.StepTarget[QueryResult[TaskListsPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[TaskListsPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListTaskListsInput, TaskListsPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft To Do Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft To Do Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft To Do Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_task_lists"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_task_lists_step, '__module__', 'dex_connectors.microsoft_todo.steps')

def new_list_tasks_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListTasksInput],
    listed: dex.StepTarget[QueryResult[TasksPage]],
    not_found: dex.StepTarget[QueryResult[TasksPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[TasksPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[TasksPage]] | None = None,
    defect: dex.StepTarget[QueryResult[TasksPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[TasksPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListTasksInput, TasksPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft To Do Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft To Do Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft To Do Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "list_tasks"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_tasks_step, '__module__', 'dex_connectors.microsoft_todo.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
