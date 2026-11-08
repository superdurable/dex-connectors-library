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
from ..models import GetPostInput, ListPostsInput, PagePostMetadata, PagePostsPage, PublishTextPostInput, PublishedPost

StepInput = TypeVar("StepInput")

def new_list_posts_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPostsInput],
    listed: dex.StepTarget[QueryResult[PagePostsPage]],
    not_found: dex.StepTarget[QueryResult[PagePostsPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PagePostsPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PagePostsPage]] | None = None,
    defect: dex.StepTarget[QueryResult[PagePostsPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PagePostsPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPostsInput, PagePostsPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Facebook Pages Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Facebook Pages Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Facebook Pages Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
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
            operation=_bound_operation(connection, "list_posts"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_posts_step, '__module__', 'dex_connectors.meta_facebook_pages.steps')

def new_get_post_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetPostInput],
    found: dex.StepTarget[QueryResult[PagePostMetadata]],
    not_found: dex.StepTarget[QueryResult[PagePostMetadata]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PagePostMetadata]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PagePostMetadata]] | None = None,
    defect: dex.StepTarget[QueryResult[PagePostMetadata]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PagePostMetadata]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetPostInput, PagePostMetadata]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Facebook Pages Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Facebook Pages Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Facebook Pages Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_post"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_post_step, '__module__', 'dex_connectors.meta_facebook_pages.steps')

def new_publish_text_post_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PublishTextPostInput],
    published: dex.StepTarget[MutationResult[PublishedPost]],
    provider_rejected: dex.StepTarget[MutationResult[PublishedPost]] | None = None,
    uncertain: dex.StepTarget[MutationResult[PublishedPost]] | None = None,
    defect: dex.StepTarget[MutationResult[PublishedPost]] | None = None,
    result_attribute: dex.Attribute[MutationResult[PublishedPost]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PublishTextPostInput, PublishedPost]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Facebook Pages Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Facebook Pages Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Facebook Pages Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "published": published,
    }
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
            operation=_bound_operation(connection, "publish_text_post"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_publish_text_post_step, '__module__', 'dex_connectors.meta_facebook_pages.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
