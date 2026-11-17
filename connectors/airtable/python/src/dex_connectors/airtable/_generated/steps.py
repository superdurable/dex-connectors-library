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
from ..models import GetRecordInput, ListRecordsInput, Record, RecordPage, UpdateRecordsInput, UpdatedRecords, UpsertRecordsInput, UpsertedRecords

StepInput = TypeVar("StepInput")

def new_list_records_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListRecordsInput],
    listed: dex.StepTarget[QueryResult[RecordPage]],
    offset_expired: dex.StepTarget[QueryResult[RecordPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[RecordPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[RecordPage]] | None = None,
    defect: dex.StepTarget[QueryResult[RecordPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[RecordPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListRecordsInput, RecordPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Airtable Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Airtable Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Airtable Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if offset_expired is not None:
        branches["offsetExpired"] = offset_expired
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
            operation=_bound_operation(connection, "list_records"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_records_step, '__module__', 'dex_connectors.airtable.steps')

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
        raise ValueError("Airtable Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Airtable Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Airtable Step annotations must be strings")
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

setattr(new_get_record_step, '__module__', 'dex_connectors.airtable.steps')

def new_upsert_records_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertRecordsInput],
    upserted: dex.StepTarget[MutationResult[UpsertedRecords]],
    provider_rejected: dex.StepTarget[MutationResult[UpsertedRecords]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpsertedRecords]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertedRecords]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertedRecords]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertRecordsInput, UpsertedRecords]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Airtable Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Airtable Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Airtable Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "upserted": upserted,
    }
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
            operation=_bound_operation(connection, "upsert_records"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_records_step, '__module__', 'dex_connectors.airtable.steps')

def new_update_records_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateRecordsInput],
    updated: dex.StepTarget[MutationResult[UpdatedRecords]],
    not_found: dex.StepTarget[MutationResult[UpdatedRecords]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdatedRecords]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdatedRecords]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdatedRecords]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdatedRecords]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateRecordsInput, UpdatedRecords]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Airtable Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Airtable Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Airtable Step annotations must be strings")
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
            operation=_bound_operation(connection, "update_records"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_records_step, '__module__', 'dex_connectors.airtable.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
