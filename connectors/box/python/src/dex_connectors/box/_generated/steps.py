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
from ..models import DownloadFileInput, DownloadedFile, UploadFileInput, UploadedFile

StepInput = TypeVar("StepInput")

def new_download_file_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], DownloadFileInput],
    downloaded: dex.StepTarget[QueryResult[DownloadedFile]],
    not_found: dex.StepTarget[QueryResult[DownloadedFile]] | None = None,
    too_large: dex.StepTarget[QueryResult[DownloadedFile]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[DownloadedFile]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[DownloadedFile]] | None = None,
    defect: dex.StepTarget[QueryResult[DownloadedFile]] | None = None,
    result_attribute: dex.Attribute[QueryResult[DownloadedFile]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, DownloadFileInput, DownloadedFile]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Box Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Box Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Box Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "downloaded": downloaded,
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
            operation=_bound_operation(connection, "download_file"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_download_file_step, '__module__', 'dex_connectors.box.steps')

def new_upload_file_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UploadFileInput],
    uploaded: dex.StepTarget[MutationResult[UploadedFile]],
    not_found: dex.StepTarget[MutationResult[UploadedFile]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UploadedFile]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UploadedFile]] | None = None,
    defect: dex.StepTarget[MutationResult[UploadedFile]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UploadedFile]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UploadFileInput, UploadedFile]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Box Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Box Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Box Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "uploaded": uploaded,
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
            operation=_bound_operation(connection, "upload_file"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_upload_file_step, '__module__', 'dex_connectors.box.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
