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
from ..models import CancelStatementInput, GetStatementResultInput, StatementCancellation, StatementResult, StatementSubmission, SubmitStatementInput

StepInput = TypeVar("StepInput")

def new_submit_statement_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SubmitStatementInput],
    submitted: dex.StepTarget[MutationResult[StatementSubmission]],
    provider_rejected: dex.StepTarget[MutationResult[StatementSubmission]] | None = None,
    defect: dex.StepTarget[MutationResult[StatementSubmission]] | None = None,
    result_attribute: dex.Attribute[MutationResult[StatementSubmission]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SubmitStatementInput, StatementSubmission]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Snowflake Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Snowflake Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Snowflake Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "submitted": submitted,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "submit_statement"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_submit_statement_step, '__module__', 'dex_connectors.snowflake.steps')

def new_get_statement_result_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetStatementResultInput],
    completed: dex.StepTarget[QueryResult[StatementResult]],
    running: dex.StepTarget[QueryResult[StatementResult]] | None = None,
    truncated: dex.StepTarget[QueryResult[StatementResult]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[StatementResult]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[StatementResult]] | None = None,
    defect: dex.StepTarget[QueryResult[StatementResult]] | None = None,
    result_attribute: dex.Attribute[QueryResult[StatementResult]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetStatementResultInput, StatementResult]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Snowflake Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Snowflake Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Snowflake Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "completed": completed,
    }
    if running is not None:
        branches["running"] = running
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
            operation=_bound_operation(connection, "get_statement_result"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_statement_result_step, '__module__', 'dex_connectors.snowflake.steps')

def new_cancel_statement_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CancelStatementInput],
    canceled: dex.StepTarget[MutationResult[StatementCancellation]],
    provider_rejected: dex.StepTarget[MutationResult[StatementCancellation]] | None = None,
    defect: dex.StepTarget[MutationResult[StatementCancellation]] | None = None,
    result_attribute: dex.Attribute[MutationResult[StatementCancellation]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CancelStatementInput, StatementCancellation]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Snowflake Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Snowflake Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Snowflake Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "canceled": canceled,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "cancel_statement"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_cancel_statement_step, '__module__', 'dex_connectors.snowflake.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
