# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    MutationResult,
    MutationStep,
    MutationStepConfig,
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import AddCommentInput, AddCommentOutput, CreateTaskInput, CreateTaskOutput, GetTaskInput, ListTasksInput, ListTasksOutput, Task, UpdateTaskInput, UpdateTaskOutput

StepInput = TypeVar("StepInput")

def new_list_tasks_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListTasksInput],
    listed: dex.StepTarget[QueryResult[ListTasksOutput]],
    not_found: dex.StepTarget[QueryResult[ListTasksOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListTasksOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListTasksOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListTasksOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListTasksOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListTasksInput, ListTasksOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Asana Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Asana Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Asana Step annotations must be strings")
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

setattr(new_list_tasks_step, '__module__', 'dex_connectors.asana.steps')

def new_get_task_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetTaskInput],
    found: dex.StepTarget[QueryResult[Task]],
    not_found: dex.StepTarget[QueryResult[Task]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Task]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Task]] | None = None,
    defect: dex.StepTarget[QueryResult[Task]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Task]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetTaskInput, Task]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Asana Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Asana Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Asana Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
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
            operation=_bound_operation(connection, "get_task"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_task_step, '__module__', 'dex_connectors.asana.steps')

def new_create_task_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateTaskInput],
    created: dex.StepTarget[MutationResult[CreateTaskOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateTaskOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateTaskOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateTaskOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateTaskOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateTaskInput, CreateTaskOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Asana Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Asana Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Asana Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_task"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_task_step, '__module__', 'dex_connectors.asana.steps')

def new_update_task_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateTaskInput],
    updated: dex.StepTarget[MutationResult[UpdateTaskOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateTaskOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateTaskOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateTaskOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateTaskOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateTaskOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateTaskInput, UpdateTaskOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Asana Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Asana Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Asana Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_task"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_task_step, '__module__', 'dex_connectors.asana.steps')

def new_add_comment_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddCommentInput],
    added: dex.StepTarget[MutationResult[AddCommentOutput]],
    not_found: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddCommentOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddCommentInput, AddCommentOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Asana Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Asana Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Asana Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "add_comment"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_comment_step, '__module__', 'dex_connectors.asana.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
