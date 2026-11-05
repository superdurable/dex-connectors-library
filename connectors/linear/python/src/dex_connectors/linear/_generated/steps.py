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
from ..models import AddCommentInput, AddCommentOutput, CreateIssueInput, CreateIssueOutput, FindUserByEmailInput, GetIssueInput, Issue, ListWorkflowStatesInput, ListWorkflowStatesOutput, SearchIssuesInput, SearchIssuesOutput, UpdateIssueInput, UpdateIssueOutput, User

StepInput = TypeVar("StepInput")

def new_search_issues_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchIssuesInput],
    searched: dex.StepTarget[QueryResult[SearchIssuesOutput]],
    provider_rejected: dex.StepTarget[QueryResult[SearchIssuesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchIssuesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchIssuesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchIssuesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchIssuesInput, SearchIssuesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
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
            operation=_bound_operation(connection, "search_issues"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_issues_step, '__module__', 'dex_connectors.linear.steps')

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
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_issue"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_issue_step, '__module__', 'dex_connectors.linear.steps')

def new_create_issue_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateIssueInput],
    created: dex.StepTarget[MutationResult[CreateIssueOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateIssueOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateIssueOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateIssueOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateIssueInput, CreateIssueOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if provider_rejected is not None:
        branches["providerRejected"] = provider_rejected
    if defect is not None:
        branches["defect"] = defect
    return MutationStep(
        MutationStepConfig(
            step_type=step_type,
            step_input_type=step_input_type,
            annotations=step_annotations,
            operation=_bound_operation(connection, "create_issue"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_issue_step, '__module__', 'dex_connectors.linear.steps')

def new_update_issue_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateIssueInput],
    updated: dex.StepTarget[MutationResult[UpdateIssueOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateIssueOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateIssueOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateIssueOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateIssueOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateIssueOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateIssueInput, UpdateIssueOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
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
            operation=_bound_operation(connection, "update_issue"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_issue_step, '__module__', 'dex_connectors.linear.steps')

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
    defect: dex.StepTarget[MutationResult[AddCommentOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddCommentOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddCommentInput, AddCommentOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
    }
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
            operation=_bound_operation(connection, "add_comment"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_comment_step, '__module__', 'dex_connectors.linear.steps')

def new_find_user_by_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindUserByEmailInput],
    found: dex.StepTarget[QueryResult[User]],
    not_found: dex.StepTarget[QueryResult[User]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[User]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[User]] | None = None,
    defect: dex.StepTarget[QueryResult[User]] | None = None,
    result_attribute: dex.Attribute[QueryResult[User]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindUserByEmailInput, User]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
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
            operation=_bound_operation(connection, "find_user_by_email"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_user_by_email_step, '__module__', 'dex_connectors.linear.steps')

def new_list_workflow_states_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListWorkflowStatesInput],
    listed: dex.StepTarget[QueryResult[ListWorkflowStatesOutput]],
    not_found: dex.StepTarget[QueryResult[ListWorkflowStatesOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListWorkflowStatesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListWorkflowStatesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListWorkflowStatesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListWorkflowStatesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListWorkflowStatesInput, ListWorkflowStatesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Linear Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Linear Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Linear Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_workflow_states"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_workflow_states_step, '__module__', 'dex_connectors.linear.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
