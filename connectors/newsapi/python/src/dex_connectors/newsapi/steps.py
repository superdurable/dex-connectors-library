# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factory for NewsAPI article searches."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import QueryResult, QueryStep, QueryStepConfig, StepAnnotations

from .connection import Connection
from .models import ArticlePage, SearchArticlesInput

StepInput = TypeVar("StepInput")


def new_search_articles_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchArticlesInput],
    searched: dex.StepTarget[QueryResult[ArticlePage]],
    provider_rejected: dex.StepTarget[QueryResult[ArticlePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ArticlePage]] | None = None,
    defect: dex.StepTarget[QueryResult[ArticlePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ArticlePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchArticlesInput, ArticlePage]:
    """Create the typed search Step; unwired optional outcomes fail the Flow."""
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("NewsAPI Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {
            "groupId",
            "groupLabel",
            "explanation",
        }:
            raise ValueError("NewsAPI Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("NewsAPI Step annotations must be strings")
        step_annotations = StepAnnotations(
            annotations["groupId"], annotations["groupLabel"], annotations["explanation"]
        )
    branches: dict[str, dex.StepTarget[QueryResult[ArticlePage]]] = {"searched": searched}
    for branch, target in (
        ("providerRejected", provider_rejected),
        ("invalidResponse", invalid_response),
        ("defect", defect),
    ):
        if target is not None:
            branches[branch] = target
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=connection.search_articles(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )
