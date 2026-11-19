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
from ..models import GetObjectTextInput, HeadObjectInput, ListObjectsInput, ListObjectsOutput, ObjectMetadata, ObjectText, PutObjectInput, StoredObject

StepInput = TypeVar("StepInput")

def new_list_objects_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListObjectsInput],
    found: dex.StepTarget[QueryResult[ListObjectsOutput]],
    not_found: dex.StepTarget[QueryResult[ListObjectsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListObjectsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListObjectsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListObjectsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListObjectsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListObjectsInput, ListObjectsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon S3 Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon S3 Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon S3 Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_objects"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_objects_step, '__module__', 'dex_connectors.amazon_s3.steps')

def new_head_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], HeadObjectInput],
    found: dex.StepTarget[QueryResult[ObjectMetadata]],
    not_found: dex.StepTarget[QueryResult[ObjectMetadata]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ObjectMetadata]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ObjectMetadata]] | None = None,
    defect: dex.StepTarget[QueryResult[ObjectMetadata]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ObjectMetadata]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, HeadObjectInput, ObjectMetadata]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon S3 Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon S3 Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon S3 Step annotations must be strings")
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
            operation=_bound_operation(connection, "head_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_head_object_step, '__module__', 'dex_connectors.amazon_s3.steps')

def new_get_object_text_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetObjectTextInput],
    read: dex.StepTarget[QueryResult[ObjectText]],
    not_found: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    unsupported_content: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    too_large: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    defect: dex.StepTarget[QueryResult[ObjectText]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ObjectText]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetObjectTextInput, ObjectText]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon S3 Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon S3 Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon S3 Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_object_text"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_object_text_step, '__module__', 'dex_connectors.amazon_s3.steps')

def new_put_object_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PutObjectInput],
    stored: dex.StepTarget[MutationResult[StoredObject]],
    already_exists: dex.StepTarget[MutationResult[StoredObject]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[StoredObject]] | None = None,
    defect: dex.StepTarget[MutationResult[StoredObject]] | None = None,
    result_attribute: dex.Attribute[MutationResult[StoredObject]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PutObjectInput, StoredObject]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Amazon S3 Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Amazon S3 Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Amazon S3 Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "stored": stored,
    }
    if already_exists is not None:
        branches["alreadyExists"] = already_exists
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "put_object"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_put_object_step, '__module__', 'dex_connectors.amazon_s3.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
