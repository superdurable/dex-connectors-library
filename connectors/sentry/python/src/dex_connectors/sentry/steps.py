# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Sentry issue operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

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

from .connection import Connection
from .models import GetIssueInput, Issue, IssuePage, ListIssuesInput, UpdateIssueStatusInput

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_list_issues_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListIssuesInput],
    listed: dex.StepTarget[QueryResult[IssuePage]],
    not_listed: dex.StepTarget[QueryResult[IssuePage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[IssuePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[IssuePage]] | None = None,
    defect: dex.StepTarget[QueryResult[IssuePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[IssuePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListIssuesInput, IssuePage]:
    """Create the typed issue search Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.list_issues(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("listed", listed),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_get_issue_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetIssueInput],
    found: dex.StepTarget[QueryResult[Issue]],
    not_found: dex.StepTarget[QueryResult[Issue]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Issue]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Issue]] | None = None,
    defect: dex.StepTarget[QueryResult[Issue]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Issue]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetIssueInput, Issue]:
    """Create the typed issue lookup Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.get_issue(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("found", found),
                ("notFound", not_found),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_update_issue_status_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateIssueStatusInput],
    updated: dex.StepTarget[MutationResult[Issue]],
    not_found: dex.StepTarget[MutationResult[Issue]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[Issue]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[Issue]] | None = None,
    defect: dex.StepTarget[MutationResult[Issue]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Issue]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateIssueStatusInput, Issue]:
    """Create the typed issue status update Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.update_issue_status(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("updated", updated),
                ("notFound", not_found),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def _require_connection(connection: Connection, connection_name: str) -> None:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Sentry Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Sentry Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Sentry Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
