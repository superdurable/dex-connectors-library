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
from ..models import ExecuteStatementInput, QueryRowsInput, RowSet, StatementExecution

StepInput = TypeVar("StepInput")


def new_query_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], QueryRowsInput],
    completed: dex.StepTarget[QueryResult[RowSet]],
    truncated: dex.StepTarget[QueryResult[RowSet]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[RowSet]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[RowSet]] | None = None,
    defect: dex.StepTarget[QueryResult[RowSet]] | None = None,
    result_attribute: dex.Attribute[QueryResult[RowSet]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, QueryRowsInput, RowSet]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("PostgreSQL Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {
            "groupId",
            "groupLabel",
            "explanation",
        }:
            raise ValueError("PostgreSQL Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("PostgreSQL Step annotations must be strings")
        step_annotations = StepAnnotations(
            annotations["groupId"], annotations["groupLabel"], annotations["explanation"]
        )
    branches = {
        "completed": completed,
    }
    if truncated is not None:
        branches["truncated"] = truncated
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
            operation=_bound_operation(connection, "query"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


setattr(new_query_step, "__module__", "dex_connectors.postgresql.steps")


def new_execute_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ExecuteStatementInput],
    completed: dex.StepTarget[MutationResult[StatementExecution]],
    limit_exceeded: dex.StepTarget[MutationResult[StatementExecution]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[StatementExecution]] | None = None,
    uncertain: dex.StepTarget[MutationResult[StatementExecution]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[StatementExecution]] | None = None,
    defect: dex.StepTarget[MutationResult[StatementExecution]] | None = None,
    result_attribute: dex.Attribute[MutationResult[StatementExecution]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ExecuteStatementInput, StatementExecution]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("PostgreSQL Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {
            "groupId",
            "groupLabel",
            "explanation",
        }:
            raise ValueError("PostgreSQL Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("PostgreSQL Step annotations must be strings")
        step_annotations = StepAnnotations(
            annotations["groupId"], annotations["groupLabel"], annotations["explanation"]
        )
    branches = {
        "completed": completed,
    }
    if limit_exceeded is not None:
        branches["limitExceeded"] = limit_exceeded
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if invalid_response is not None:
        branches["invalidResponse"] = invalid_response
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "execute"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


setattr(new_execute_step, "__module__", "dex_connectors.postgresql.steps")


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
