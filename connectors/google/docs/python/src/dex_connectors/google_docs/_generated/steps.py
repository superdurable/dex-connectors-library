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
from ..models import AppendTextInput, AppendTextOutput, CreateDocumentInput, CreateDocumentOutput, GetDocumentTextInput, GetDocumentTextOutput, ReplaceDocumentTextInput, ReplaceDocumentTextOutput

StepInput = TypeVar("StepInput")

def new_get_document_text_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetDocumentTextInput],
    read: dex.StepTarget[QueryResult[GetDocumentTextOutput]],
    not_found: dex.StepTarget[QueryResult[GetDocumentTextOutput]] | None = None,
    too_large: dex.StepTarget[QueryResult[GetDocumentTextOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[GetDocumentTextOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[GetDocumentTextOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[GetDocumentTextOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[GetDocumentTextOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetDocumentTextInput, GetDocumentTextOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Docs Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Docs Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Docs Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "read": read,
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
            operation=_bound_operation(connection, "get_document_text"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_document_text_step, '__module__', 'dex_connectors.google_docs.steps')

def new_create_document_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateDocumentInput],
    created: dex.StepTarget[MutationResult[CreateDocumentOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateDocumentOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CreateDocumentOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateDocumentOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateDocumentOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateDocumentOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateDocumentInput, CreateDocumentOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Docs Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Docs Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Docs Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
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
            operation=_bound_operation(connection, "create_document"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_document_step, '__module__', 'dex_connectors.google_docs.steps')

def new_replace_document_text_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ReplaceDocumentTextInput],
    replaced: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]],
    placeholder_not_found: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    revision_changed: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    not_found: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ReplaceDocumentTextOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, ReplaceDocumentTextInput, ReplaceDocumentTextOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Docs Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Docs Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Docs Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "replaced": replaced,
    }
    if placeholder_not_found is not None:
        branches["placeholderNotFound"] = placeholder_not_found
    if revision_changed is not None:
        branches["revisionChanged"] = revision_changed
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
            operation=_bound_operation(connection, "replace_document_text"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_replace_document_text_step, '__module__', 'dex_connectors.google_docs.steps')

def new_append_text_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AppendTextInput],
    appended: dex.StepTarget[MutationResult[AppendTextOutput]],
    revision_changed: dex.StepTarget[MutationResult[AppendTextOutput]] | None = None,
    not_found: dex.StepTarget[MutationResult[AppendTextOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AppendTextOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[AppendTextOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AppendTextOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AppendTextOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AppendTextInput, AppendTextOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Docs Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Docs Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Docs Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "appended": appended,
    }
    if revision_changed is not None:
        branches["revisionChanged"] = revision_changed
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
            operation=_bound_operation(connection, "append_text"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_append_text_step, '__module__', 'dex_connectors.google_docs.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
