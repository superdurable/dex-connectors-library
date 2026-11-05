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
from ..models import AddUserToGroupInput, AddUserToGroupOutput, CreateUserInput, CreateUserOutput, DisableUserInput, EnableUserInput, GetUserInput, ListUsersInput, ListUsersOutput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput, RevokeSignInSessionsInput, RevokeSignInSessionsOutput, User

StepInput = TypeVar("StepInput")

def new_get_user_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetUserInput],
    found: dex.StepTarget[QueryResult[User]],
    not_found: dex.StepTarget[QueryResult[User]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[User]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[User]] | None = None,
    defect: dex.StepTarget[QueryResult[User]] | None = None,
    result_attribute: dex.Attribute[QueryResult[User]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetUserInput, User]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_user"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_user_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_list_users_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListUsersInput],
    listed: dex.StepTarget[QueryResult[ListUsersOutput]],
    provider_rejected: dex.StepTarget[QueryResult[ListUsersOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListUsersOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListUsersOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListUsersOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListUsersInput, ListUsersOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_users"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_users_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_create_user_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateUserInput],
    created: dex.StepTarget[MutationResult[CreateUserOutput]],
    already_exists: dex.StepTarget[MutationResult[CreateUserOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[CreateUserOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CreateUserOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateUserOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateUserOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateUserInput, CreateUserOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
    if already_exists is not None:
        branches["alreadyExists"] = already_exists
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
            operation=_bound_operation(connection, "create_user"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_user_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_disable_user_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], DisableUserInput],
    disabled: dex.StepTarget[MutationResult[User]],
    not_found: dex.StepTarget[MutationResult[User]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[User]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[User]] | None = None,
    defect: dex.StepTarget[MutationResult[User]] | None = None,
    result_attribute: dex.Attribute[MutationResult[User]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, DisableUserInput, User]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "disabled": disabled,
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
            operation=_bound_operation(connection, "disable_user"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_disable_user_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_enable_user_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], EnableUserInput],
    enabled: dex.StepTarget[MutationResult[User]],
    not_found: dex.StepTarget[MutationResult[User]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[User]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[User]] | None = None,
    defect: dex.StepTarget[MutationResult[User]] | None = None,
    result_attribute: dex.Attribute[MutationResult[User]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, EnableUserInput, User]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "enabled": enabled,
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
            operation=_bound_operation(connection, "enable_user"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_enable_user_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_revoke_sign_in_sessions_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RevokeSignInSessionsInput],
    revoked: dex.StepTarget[MutationResult[RevokeSignInSessionsOutput]],
    not_found: dex.StepTarget[MutationResult[RevokeSignInSessionsOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[RevokeSignInSessionsOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[RevokeSignInSessionsOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[RevokeSignInSessionsOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[RevokeSignInSessionsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, RevokeSignInSessionsInput, RevokeSignInSessionsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "revoked": revoked,
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
            operation=_bound_operation(connection, "revoke_sign_in_sessions"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_revoke_sign_in_sessions_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_add_user_to_group_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddUserToGroupInput],
    added: dex.StepTarget[MutationResult[AddUserToGroupOutput]],
    not_found: dex.StepTarget[MutationResult[AddUserToGroupOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddUserToGroupOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[AddUserToGroupOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddUserToGroupOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddUserToGroupOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddUserToGroupInput, AddUserToGroupOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
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
            operation=_bound_operation(connection, "add_user_to_group"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_user_to_group_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')

def new_remove_user_from_group_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], RemoveUserFromGroupInput],
    removed: dex.StepTarget[MutationResult[RemoveUserFromGroupOutput]],
    not_found: dex.StepTarget[MutationResult[RemoveUserFromGroupOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[RemoveUserFromGroupOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[RemoveUserFromGroupOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[RemoveUserFromGroupOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[RemoveUserFromGroupOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, RemoveUserFromGroupInput, RemoveUserFromGroupOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Entra ID Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Entra ID Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Entra ID Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "removed": removed,
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
            operation=_bound_operation(connection, "remove_user_from_group"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_remove_user_from_group_step, '__module__', 'dex_connectors.microsoft_entra_id.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
