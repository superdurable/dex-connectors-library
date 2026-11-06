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
from ..models import FindRowInput, FindRowOutput, GetValuesInput, GetValuesOutput, UpsertRowInput, UpsertRowOutput

StepInput = TypeVar("StepInput")

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
    provider_rejected: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[GetValuesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[GetValuesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetValuesInput, GetValuesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Sheets Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Sheets Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Sheets Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
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
            operation=_bound_operation(connection, "get_values"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_values_step, '__module__', 'dex_connectors.google_spreadsheet.steps')

def new_find_row_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindRowInput],
    found: dex.StepTarget[QueryResult[FindRowOutput]],
    not_found: dex.StepTarget[QueryResult[FindRowOutput]] | None = None,
    conflict: dex.StepTarget[QueryResult[FindRowOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FindRowOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FindRowOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[FindRowOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FindRowOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindRowInput, FindRowOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Sheets Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Sheets Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Sheets Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if conflict is not None:
        branches["conflict"] = conflict
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
            operation=_bound_operation(connection, "find_row"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_row_step, '__module__', 'dex_connectors.google_spreadsheet.steps')

def new_upsert_row_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertRowInput],
    upserted: dex.StepTarget[MutationResult[UpsertRowOutput]],
    conflict: dex.StepTarget[MutationResult[UpsertRowOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpsertRowOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpsertRowOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[UpsertRowOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertRowOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertRowOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertRowInput, UpsertRowOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Sheets Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Sheets Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Sheets Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "upserted": upserted,
    }
    if conflict is not None:
        branches["conflict"] = conflict
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
            operation=_bound_operation(connection, "upsert_row"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_row_step, '__module__', 'dex_connectors.google_spreadsheet.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
