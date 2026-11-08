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
from ..models import DownloadObjectInput, DownloadedObject, ListObjectsInput, Object, ObjectsPage, UploadObjectInput

StepInput = TypeVar("StepInput")

def new_list_objects_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListObjectsInput],
    listed: dex.StepTarget[QueryResult[ObjectsPage]],
    provider_rejected: dex.StepTarget[QueryResult[ObjectsPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ObjectsPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ObjectsPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ObjectsPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListObjectsInput, ObjectsPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Cloud Storage Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Cloud Storage Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Cloud Storage Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_objects"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_objects_step, '__module__', 'dex_connectors.google_cloud_storage.steps')

def new_download_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], DownloadObjectInput],
    downloaded: dex.StepTarget[QueryResult[DownloadedObject]],
    not_found: dex.StepTarget[QueryResult[DownloadedObject]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[DownloadedObject]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[DownloadedObject]] | None = None,
    defect: dex.StepTarget[QueryResult[DownloadedObject]] | None = None,
    result_attribute: dex.Attribute[QueryResult[DownloadedObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, DownloadObjectInput, DownloadedObject]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Cloud Storage Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Cloud Storage Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Cloud Storage Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "downloaded": downloaded,
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
            operation=_bound_operation(connection, "download_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_download_object_step, '__module__', 'dex_connectors.google_cloud_storage.steps')

def new_upload_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UploadObjectInput],
    uploaded: dex.StepTarget[MutationResult[Object]],
    object_exists: dex.StepTarget[MutationResult[Object]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[Object]] | None = None,
    defect: dex.StepTarget[MutationResult[Object]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Object]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UploadObjectInput, Object]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Cloud Storage Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Cloud Storage Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Cloud Storage Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "uploaded": uploaded,
    }
    if object_exists is not None:
        branches["objectExists"] = object_exists
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "upload_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upload_object_step, '__module__', 'dex_connectors.google_cloud_storage.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
