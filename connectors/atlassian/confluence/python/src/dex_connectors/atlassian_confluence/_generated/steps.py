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
from ..models import AddCommentInput, AddCommentOutput, CreatePageInput, CreatePageOutput, GetPageInput, Page, SearchPagesInput, SearchPagesOutput, UpdatePageInput, UpdatePageOutput

StepInput = TypeVar("StepInput")

def new_search_pages_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchPagesInput],
    searched: dex.StepTarget[QueryResult[SearchPagesOutput]],
    provider_rejected: dex.StepTarget[QueryResult[SearchPagesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchPagesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchPagesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchPagesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchPagesInput, SearchPagesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Confluence Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Confluence Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Confluence Step annotations must be strings")
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
            operation=_bound_operation(connection, "search_pages"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_pages_step, '__module__', 'dex_connectors.atlassian_confluence.steps')

def new_get_page_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetPageInput],
    found: dex.StepTarget[QueryResult[Page]],
    not_found: dex.StepTarget[QueryResult[Page]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Page]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Page]] | None = None,
    defect: dex.StepTarget[QueryResult[Page]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Page]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetPageInput, Page]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Confluence Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Confluence Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Confluence Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_page"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_page_step, '__module__', 'dex_connectors.atlassian_confluence.steps')

def new_create_page_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreatePageInput],
    created: dex.StepTarget[MutationResult[CreatePageOutput]],
    title_conflict: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    not_found: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreatePageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreatePageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreatePageInput, CreatePageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Confluence Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Confluence Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Confluence Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if title_conflict is not None:
        branches["titleConflict"] = title_conflict
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_page"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_page_step, '__module__', 'dex_connectors.atlassian_confluence.steps')

def new_update_page_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdatePageInput],
    updated: dex.StepTarget[MutationResult[UpdatePageOutput]],
    version_conflict: dex.StepTarget[MutationResult[UpdatePageOutput]] | None = None,
    not_found: dex.StepTarget[MutationResult[UpdatePageOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdatePageOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdatePageOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdatePageOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdatePageInput, UpdatePageOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Confluence Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Confluence Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Confluence Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
    }
    if version_conflict is not None:
        branches["versionConflict"] = version_conflict
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "update_page"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_page_step, '__module__', 'dex_connectors.atlassian_confluence.steps')

def new_add_comment_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddCommentInput],
    added: dex.StepTarget[MutationResult[AddCommentOutput]],
    not_found: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddCommentOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddCommentInput, AddCommentOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Confluence Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Confluence Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Confluence Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if uncertain is not None:
        branches["uncertain"] = uncertain
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "add_comment"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_comment_step, '__module__', 'dex_connectors.atlassian_confluence.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
