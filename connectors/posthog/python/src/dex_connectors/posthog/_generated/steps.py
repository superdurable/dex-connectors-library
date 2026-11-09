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
from ..models import CaptureEventInput, CapturedEvent, InsightsPage, ListInsightsInput

StepInput = TypeVar("StepInput")

def new_capture_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CaptureEventInput],
    accepted: dex.StepTarget[MutationResult[CapturedEvent]],
    quota_limited: dex.StepTarget[MutationResult[CapturedEvent]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CapturedEvent]] | None = None,
    defect: dex.StepTarget[MutationResult[CapturedEvent]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CapturedEvent]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CaptureEventInput, CapturedEvent]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("PostHog Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("PostHog Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("PostHog Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "accepted": accepted,
    }
    if quota_limited is not None:
        branches["quotaLimited"] = quota_limited
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "capture_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_capture_event_step, '__module__', 'dex_connectors.posthog.steps')

def new_list_insights_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListInsightsInput],
    listed: dex.StepTarget[QueryResult[InsightsPage]],
    provider_rejected: dex.StepTarget[QueryResult[InsightsPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[InsightsPage]] | None = None,
    defect: dex.StepTarget[QueryResult[InsightsPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[InsightsPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListInsightsInput, InsightsPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("PostHog Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("PostHog Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("PostHog Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_insights"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_insights_step, '__module__', 'dex_connectors.posthog.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
