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
from ..models import CreateFolderInput, CreateFolderOutput, DriveItem, GetFileInput, ReadFileTextInput, ReadFileTextOutput, SearchFilesInput, SearchFilesOutput, UploadFileInput, UploadFileOutput

StepInput = TypeVar("StepInput")

def new_search_files_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchFilesInput],
    found: dex.StepTarget[QueryResult[SearchFilesOutput]],
    not_found: dex.StepTarget[QueryResult[SearchFilesOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[SearchFilesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchFilesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchFilesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchFilesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchFilesInput, SearchFilesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft OneDrive and SharePoint Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations must be strings")
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
            operation=_bound_operation(connection, "search_files"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_files_step, '__module__', 'dex_connectors.microsoft_onedrive.steps')

def new_get_file_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetFileInput],
    found: dex.StepTarget[QueryResult[DriveItem]],
    not_found: dex.StepTarget[QueryResult[DriveItem]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[DriveItem]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[DriveItem]] | None = None,
    defect: dex.StepTarget[QueryResult[DriveItem]] | None = None,
    result_attribute: dex.Attribute[QueryResult[DriveItem]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetFileInput, DriveItem]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft OneDrive and SharePoint Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_file"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_file_step, '__module__', 'dex_connectors.microsoft_onedrive.steps')

def new_read_file_text_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReadFileTextInput],
    read: dex.StepTarget[QueryResult[ReadFileTextOutput]],
    not_found: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    unsupported_content: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    too_large: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ReadFileTextOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ReadFileTextOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ReadFileTextInput, ReadFileTextOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft OneDrive and SharePoint Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if unsupported_content is not None:
        branches["unsupportedContent"] = unsupported_content
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
            operation=_bound_operation(connection, "read_file_text"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_read_file_text_step, '__module__', 'dex_connectors.microsoft_onedrive.steps')

def new_upload_file_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UploadFileInput],
    uploaded: dex.StepTarget[MutationResult[UploadFileOutput]],
    already_exists: dex.StepTarget[MutationResult[UploadFileOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UploadFileOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UploadFileOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UploadFileOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UploadFileOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UploadFileInput, UploadFileOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft OneDrive and SharePoint Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "uploaded": uploaded,
    }
    if already_exists is not None:
        branches["alreadyExists"] = already_exists
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

setattr(new_upload_file_step, '__module__', 'dex_connectors.microsoft_onedrive.steps')

def new_create_folder_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateFolderInput],
    created: dex.StepTarget[MutationResult[CreateFolderOutput]],
    name_conflict: dex.StepTarget[MutationResult[CreateFolderOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreateFolderOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CreateFolderOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateFolderOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateFolderOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateFolderInput, CreateFolderOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft OneDrive and SharePoint Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft OneDrive and SharePoint Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if name_conflict is not None:
        branches["nameConflict"] = name_conflict
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
            operation=_bound_operation(connection, "create_folder"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_folder_step, '__module__', 'dex_connectors.microsoft_onedrive.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
