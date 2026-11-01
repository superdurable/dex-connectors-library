# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Operation-specific Dex Step factories for Bitbucket repository operations."""

from collections.abc import Callable, Mapping
from typing import TypeVar

import dex
from dex_connectors.sdk import QueryResult, QueryStep, QueryStepConfig, StepAnnotations

from .connection import Connection
from .models import ListPullRequestsInput, ListRepositoriesInput, PullRequestPage, RepositoryPage

StepInput = TypeVar("StepInput")
BranchTarget = TypeVar("BranchTarget")


def new_list_repositories_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListRepositoriesInput],
    listed: dex.StepTarget[QueryResult[RepositoryPage]],
    not_found: dex.StepTarget[QueryResult[RepositoryPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[RepositoryPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[RepositoryPage]] | None = None,
    defect: dex.StepTarget[QueryResult[RepositoryPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[RepositoryPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListRepositoriesInput, RepositoryPage]:
    """Create the typed repository page Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.list_repositories(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("listed", listed),
                ("notFound", not_found),
                ("providerRejected", provider_rejected),
                ("invalidResponse", invalid_response),
                ("defect", defect),
            ),
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )


def new_list_pull_requests_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPullRequestsInput],
    listed: dex.StepTarget[QueryResult[PullRequestPage]],
    not_found: dex.StepTarget[QueryResult[PullRequestPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PullRequestPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PullRequestPage]] | None = None,
    defect: dex.StepTarget[QueryResult[PullRequestPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PullRequestPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPullRequestsInput, PullRequestPage]:
    """Create the typed pull request page Step; unwired optional outcomes fail the Flow."""
    _require_connection(connection, connection_name)
    return QueryStep(
        QueryStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=_step_annotations(annotations),
            operation=connection.list_pull_requests(),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=_branches(
                ("listed", listed),
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
        raise ValueError("Bitbucket Step connection name must match its connection reference")


def _step_annotations(annotations: StepAnnotations | Mapping[str, str]) -> StepAnnotations:
    if isinstance(annotations, StepAnnotations):
        return annotations
    if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
        raise ValueError("Bitbucket Step annotations require groupId, groupLabel, and explanation")
    if any(not isinstance(value, str) for value in annotations.values()):
        raise ValueError("Bitbucket Step annotations must be strings")
    return StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])


def _branches(*targets: tuple[str, BranchTarget | None]) -> dict[str, BranchTarget]:
    return {branch: target for branch, target in targets if target is not None}
