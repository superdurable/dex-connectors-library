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
from ..models import ListScheduledPostsInput, ListSocialProfilesInput, SchedulePostInput, ScheduledPostReceipt, ScheduledPostsPage, SocialProfilesPage

StepInput = TypeVar("StepInput")

def new_list_social_profiles_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListSocialProfilesInput],
    listed: dex.StepTarget[QueryResult[SocialProfilesPage]],
    provider_rejected: dex.StepTarget[QueryResult[SocialProfilesPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SocialProfilesPage]] | None = None,
    defect: dex.StepTarget[QueryResult[SocialProfilesPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SocialProfilesPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListSocialProfilesInput, SocialProfilesPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hootsuite Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hootsuite Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hootsuite Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_social_profiles"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_social_profiles_step, '__module__', 'dex_connectors.hootsuite.steps')

def new_list_scheduled_posts_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListScheduledPostsInput],
    listed: dex.StepTarget[QueryResult[ScheduledPostsPage]],
    provider_rejected: dex.StepTarget[QueryResult[ScheduledPostsPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ScheduledPostsPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ScheduledPostsPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ScheduledPostsPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListScheduledPostsInput, ScheduledPostsPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hootsuite Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hootsuite Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hootsuite Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_scheduled_posts"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_scheduled_posts_step, '__module__', 'dex_connectors.hootsuite.steps')

def new_schedule_post_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SchedulePostInput],
    scheduled: dex.StepTarget[MutationResult[ScheduledPostReceipt]],
    provider_rejected: dex.StepTarget[MutationResult[ScheduledPostReceipt]] | None = None,
    uncertain: dex.StepTarget[MutationResult[ScheduledPostReceipt]] | None = None,
    defect: dex.StepTarget[MutationResult[ScheduledPostReceipt]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ScheduledPostReceipt]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, SchedulePostInput, ScheduledPostReceipt]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Hootsuite Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Hootsuite Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Hootsuite Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "scheduled": scheduled,
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
            operation=_bound_operation(connection, "schedule_post"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_schedule_post_step, '__module__', 'dex_connectors.hootsuite.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
