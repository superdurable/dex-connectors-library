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
from ..models import AddUpdateInput, AddUpdateOutput, CreateItemInput, CreateItemOutput, GetItemInput, Item, ListItemsInput, ListItemsOutput, UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput

StepInput = TypeVar("StepInput")

def new_list_items_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListItemsInput],
    listed: dex.StepTarget[QueryResult[ListItemsOutput]],
    not_found: dex.StepTarget[QueryResult[ListItemsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListItemsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListItemsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListItemsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListItemsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListItemsInput, ListItemsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("monday.com Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("monday.com Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("monday.com Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_items"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_items_step, '__module__', 'dex_connectors.monday.steps')

def new_get_item_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetItemInput],
    found: dex.StepTarget[QueryResult[Item]],
    not_found: dex.StepTarget[QueryResult[Item]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Item]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Item]] | None = None,
    defect: dex.StepTarget[QueryResult[Item]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Item]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetItemInput, Item]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("monday.com Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("monday.com Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("monday.com Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_item"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_item_step, '__module__', 'dex_connectors.monday.steps')

def new_create_item_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateItemInput],
    created: dex.StepTarget[MutationResult[CreateItemOutput]],
    not_found: dex.StepTarget[MutationResult[CreateItemOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreateItemOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateItemOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateItemOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateItemOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateItemInput, CreateItemOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("monday.com Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("monday.com Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("monday.com Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
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
            operation=_bound_operation(connection, "create_item"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_item_step, '__module__', 'dex_connectors.monday.steps')

def new_update_item_column_values_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateItemColumnValuesInput],
    updated: dex.StepTarget[MutationResult[UpdateItemColumnValuesOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateItemColumnValuesOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateItemColumnValuesOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateItemColumnValuesOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateItemColumnValuesOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateItemColumnValuesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateItemColumnValuesInput, UpdateItemColumnValuesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("monday.com Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("monday.com Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("monday.com Step annotations must be strings")
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
            operation=_bound_operation(connection, "update_item_column_values"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_item_column_values_step, '__module__', 'dex_connectors.monday.steps')

def new_add_update_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddUpdateInput],
    added: dex.StepTarget[MutationResult[AddUpdateOutput]],
    not_found: dex.StepTarget[MutationResult[AddUpdateOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddUpdateOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddUpdateOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddUpdateOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddUpdateOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddUpdateInput, AddUpdateOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("monday.com Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("monday.com Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("monday.com Step annotations must be strings")
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
            operation=_bound_operation(connection, "add_update"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_update_step, '__module__', 'dex_connectors.monday.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
