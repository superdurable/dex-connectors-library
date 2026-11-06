# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from collections.abc import Callable, Mapping
from typing import Any, TypeVar

import dex
from dex_connectors.sdk import (
    QueryResult,
    QueryStep,
    QueryStepConfig,
    StepAnnotations,
)

from ..connection import Connection
from ..models import AuthenticatedProfile, CommitPage, GetAuthenticatedProfileInput, ListCommitsInput, ListMergedPullRequestsInput, ListPublicRepositoriesInput, ListPullRequestFilesInput, ListReleasesInput, MergedPullRequestPage, PublicRepositories, PullRequestFilePage, ReleasePage

StepInput = TypeVar("StepInput")

def new_get_authenticated_profile_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetAuthenticatedProfileInput],
    profile_loaded: dex.StepTarget[QueryResult[AuthenticatedProfile]],
    verified_email_required: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    insufficient_scope: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    not_found: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    defect: dex.StepTarget[QueryResult[AuthenticatedProfile]] | None = None,
    result_attribute: dex.Attribute[QueryResult[AuthenticatedProfile]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetAuthenticatedProfileInput, AuthenticatedProfile]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "profileLoaded": profile_loaded,
    }
    if verified_email_required is not None:
        branches["verifiedEmailRequired"] = verified_email_required
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "get_authenticated_profile"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_authenticated_profile_step, '__module__', 'dex_connectors.github.steps')

def new_list_public_repositories_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPublicRepositoriesInput],
    repositories_loaded: dex.StepTarget[QueryResult[PublicRepositories]],
    insufficient_scope: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    not_found: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    defect: dex.StepTarget[QueryResult[PublicRepositories]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PublicRepositories]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPublicRepositoriesInput, PublicRepositories]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "repositoriesLoaded": repositories_loaded,
    }
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "list_public_repositories"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_public_repositories_step, '__module__', 'dex_connectors.github.steps')

def new_list_merged_pull_requests_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListMergedPullRequestsInput],
    listed: dex.StepTarget[QueryResult[MergedPullRequestPage]],
    insufficient_scope: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    not_found: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    defect: dex.StepTarget[QueryResult[MergedPullRequestPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[MergedPullRequestPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListMergedPullRequestsInput, MergedPullRequestPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "list_merged_pull_requests"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_merged_pull_requests_step, '__module__', 'dex_connectors.github.steps')

def new_list_pull_request_files_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPullRequestFilesInput],
    listed: dex.StepTarget[QueryResult[PullRequestFilePage]],
    insufficient_scope: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    not_found: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    defect: dex.StepTarget[QueryResult[PullRequestFilePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[PullRequestFilePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPullRequestFilesInput, PullRequestFilePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "list_pull_request_files"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_pull_request_files_step, '__module__', 'dex_connectors.github.steps')

def new_list_releases_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListReleasesInput],
    listed: dex.StepTarget[QueryResult[ReleasePage]],
    insufficient_scope: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    not_found: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    defect: dex.StepTarget[QueryResult[ReleasePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ReleasePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListReleasesInput, ReleasePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "list_releases"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_releases_step, '__module__', 'dex_connectors.github.steps')

def new_list_commits_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListCommitsInput],
    listed: dex.StepTarget[QueryResult[CommitPage]],
    insufficient_scope: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    authorization_revoked: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    not_found: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    defect: dex.StepTarget[QueryResult[CommitPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[CommitPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListCommitsInput, CommitPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("GitHub Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("GitHub Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("GitHub Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "listed": listed,
    }
    if insufficient_scope is not None:
        branches["insufficientScope"] = insufficient_scope
    if authorization_revoked is not None:
        branches["authorizationRevoked"] = authorization_revoked
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
            operation=_bound_operation(connection, "list_commits"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_commits_step, '__module__', 'dex_connectors.github.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
