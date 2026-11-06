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
from ..models import CRMObject, GetObjectInput, ObjectPage, SearchObjectsInput, UpdateObjectInput, UpsertObjectInput, UpsertedObject

StepInput = TypeVar("StepInput")

def new_search_objects_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchObjectsInput],
    searched: dex.StepTarget[QueryResult[ObjectPage]],
    provider_rejected: dex.StepTarget[QueryResult[ObjectPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ObjectPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ObjectPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ObjectPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchObjectsInput, ObjectPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("HubSpot Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("HubSpot Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("HubSpot Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "searched": searched,
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
            operation=_bound_operation(connection, "search_objects"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_objects_step, '__module__', 'dex_connectors.hubspot.steps')

def new_get_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetObjectInput],
    found: dex.StepTarget[QueryResult[CRMObject]],
    not_found: dex.StepTarget[QueryResult[CRMObject]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[CRMObject]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[CRMObject]] | None = None,
    defect: dex.StepTarget[QueryResult[CRMObject]] | None = None,
    result_attribute: dex.Attribute[QueryResult[CRMObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetObjectInput, CRMObject]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("HubSpot Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("HubSpot Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("HubSpot Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_object_step, '__module__', 'dex_connectors.hubspot.steps')

def new_upsert_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpsertObjectInput],
    upserted: dex.StepTarget[MutationResult[UpsertedObject]],
    conflict: dex.StepTarget[MutationResult[UpsertedObject]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpsertedObject]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpsertedObject]] | None = None,
    defect: dex.StepTarget[MutationResult[UpsertedObject]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpsertedObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpsertObjectInput, UpsertedObject]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("HubSpot Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("HubSpot Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("HubSpot Step annotations must be strings")
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
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "upsert_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upsert_object_step, '__module__', 'dex_connectors.hubspot.steps')

def new_update_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateObjectInput],
    updated: dex.StepTarget[MutationResult[CRMObject]],
    not_found: dex.StepTarget[MutationResult[CRMObject]] | None = None,
    conflict: dex.StepTarget[MutationResult[CRMObject]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CRMObject]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CRMObject]] | None = None,
    defect: dex.StepTarget[MutationResult[CRMObject]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CRMObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateObjectInput, CRMObject]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("HubSpot Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("HubSpot Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("HubSpot Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
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
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_object_step, '__module__', 'dex_connectors.hubspot.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
