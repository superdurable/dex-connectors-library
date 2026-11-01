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
from ..models import CachePurge, ListZonesInput, PurgeCacheByURLsInput, ZonesPage

StepInput = TypeVar("StepInput")

def new_purge_cache_by_urls_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], PurgeCacheByURLsInput],
    purged: dex.StepTarget[MutationResult[CachePurge]],
    not_found: dex.StepTarget[MutationResult[CachePurge]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CachePurge]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CachePurge]] | None = None,
    defect: dex.StepTarget[MutationResult[CachePurge]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CachePurge]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, PurgeCacheByURLsInput, CachePurge]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Cloudflare Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Cloudflare Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Cloudflare Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "purged": purged,
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
            operation=_bound_operation(connection, "purge_cache_by_urls"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_purge_cache_by_urls_step, '__module__', 'dex_connectors.cloudflare.steps')

def new_list_zones_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListZonesInput],
    listed: dex.StepTarget[QueryResult[ZonesPage]],
    provider_rejected: dex.StepTarget[QueryResult[ZonesPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ZonesPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ZonesPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ZonesPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListZonesInput, ZonesPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Cloudflare Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Cloudflare Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Cloudflare Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_zones"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_zones_step, '__module__', 'dex_connectors.cloudflare.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
