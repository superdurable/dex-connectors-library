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
from ..models import AppendTableRowsInput, AppendTableRowsOutput, GetTableRowsInput, GetTableRowsOutput, GetValuesInput, GetValuesOutput, UpdateValuesInput, UpdateValuesOutput

StepInput = TypeVar("StepInput")

def new_get_table_rows_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetTableRowsInput],
    read: dex.StepTarget[QueryResult[GetTableRowsOutput]],
    not_found: dex.StepTarget[QueryResult[GetTableRowsOutput]] | None = None,
    too_large: dex.StepTarget[QueryResult[GetTableRowsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[GetTableRowsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GetTableRowsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[GetTableRowsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[GetTableRowsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetTableRowsInput, GetTableRowsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Excel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Excel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Excel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if too_large is not None:
        branches["tooLarge"] = too_large
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
            operation=_bound_operation(connection, "get_table_rows"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_table_rows_step, '__module__', 'dex_connectors.microsoft_excel.steps')

def new_get_values_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetValuesInput],
    read: dex.StepTarget[QueryResult[GetValuesOutput]],
    not_found: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    too_large: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[GetValuesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetValuesInput, GetValuesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Excel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Excel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Excel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if too_large is not None:
        branches["tooLarge"] = too_large
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
            operation=_bound_operation(connection, "get_values"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_values_step, '__module__', 'dex_connectors.microsoft_excel.steps')

def new_update_values_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateValuesInput],
    updated: dex.StepTarget[MutationResult[UpdateValuesOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateValuesOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateValuesOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateValuesOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateValuesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateValuesInput, UpdateValuesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Excel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Excel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Excel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_values"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_values_step, '__module__', 'dex_connectors.microsoft_excel.steps')

def new_append_table_rows_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AppendTableRowsInput],
    appended: dex.StepTarget[MutationResult[AppendTableRowsOutput]],
    not_found: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    columns_mismatch: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AppendTableRowsOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AppendTableRowsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AppendTableRowsInput, AppendTableRowsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Excel Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Excel Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Excel Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "appended": appended,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if columns_mismatch is not None:
        branches["columnsMismatch"] = columns_mismatch
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "append_table_rows"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_append_table_rows_step, '__module__', 'dex_connectors.microsoft_excel.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
