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
from ..models import GetRecordInput, QueryRecordsInput, QueryRecordsOutput, Record, UpdateRecordInput, UpdateRecordOutput, UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput

StepInput = TypeVar("StepInput")

def new_query_records_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], QueryRecordsInput],
    found: dex.StepTarget[QueryResult[QueryRecordsOutput]],
    not_found: dex.StepTarget[QueryResult[QueryRecordsOutput]] | None = None,
    query_rejected: dex.StepTarget[QueryResult[QueryRecordsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[QueryRecordsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[QueryRecordsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[QueryRecordsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[QueryRecordsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, QueryRecordsInput, QueryRecordsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Salesforce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Salesforce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Salesforce Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if query_rejected is not None:
        branches["queryRejected"] = query_rejected
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
            operation=_bound_operation(connection, "query_records"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_query_records_step, '__module__', 'dex_connectors.salesforce.steps')

def new_get_record_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetRecordInput],
    found: dex.StepTarget[QueryResult[Record]],
    not_found: dex.StepTarget[QueryResult[Record]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Record]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Record]] | None = None,
    defect: dex.StepTarget[QueryResult[Record]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Record]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetRecordInput, Record]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Salesforce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Salesforce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Salesforce Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_record"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_record_step, '__module__', 'dex_connectors.salesforce.steps')

def new_upsert_record_by_external_id_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertRecordByExternalIDInput],
    upserted: dex.StepTarget[MutationResult[UpsertRecordByExternalIDOutput]],
    multiple_matches: dex.StepTarget[MutationResult[UpsertRecordByExternalIDOutput]] | None = None,
    record_rejected: dex.StepTarget[MutationResult[UpsertRecordByExternalIDOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpsertRecordByExternalIDOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertRecordByExternalIDOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertRecordByExternalIDOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertRecordByExternalIDInput, UpsertRecordByExternalIDOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Salesforce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Salesforce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Salesforce Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "upserted": upserted,
    }
    if multiple_matches is not None:
        branches["multipleMatches"] = multiple_matches
    if record_rejected is not None:
        branches["recordRejected"] = record_rejected
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "upsert_record_by_external_id"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_record_by_external_id_step, '__module__', 'dex_connectors.salesforce.steps')

def new_update_record_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateRecordInput],
    updated: dex.StepTarget[MutationResult[UpdateRecordOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateRecordOutput]] | None = None,
    record_rejected: dex.StepTarget[MutationResult[UpdateRecordOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateRecordOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateRecordOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateRecordOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateRecordInput, UpdateRecordOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Salesforce Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Salesforce Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Salesforce Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if record_rejected is not None:
        branches["recordRejected"] = record_rejected
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_record"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_record_step, '__module__', 'dex_connectors.salesforce.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
