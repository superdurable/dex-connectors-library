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
from ..models import AddEmployeeInput, AddEmployeeOutput, EmployeeRecord, FindEmployeeByEmailInput, FindEmployeeByEmailOutput, GetEmployeeInput, ListEmployeeChangesInput, ListEmployeeChangesOutput, ListTimeOffRequestsInput, ListTimeOffRequestsOutput, UpdateEmployeeInput, UpdateEmployeeOutput

StepInput = TypeVar("StepInput")

def new_get_employee_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetEmployeeInput],
    found: dex.StepTarget[QueryResult[EmployeeRecord]],
    not_found: dex.StepTarget[QueryResult[EmployeeRecord]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[EmployeeRecord]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[EmployeeRecord]] | None = None,
    defect: dex.StepTarget[QueryResult[EmployeeRecord]] | None = None,
    result_attribute: dex.Attribute[QueryResult[EmployeeRecord]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetEmployeeInput, EmployeeRecord]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_employee"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_employee_step, '__module__', 'dex_connectors.bamboohr.steps')

def new_find_employee_by_email_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], FindEmployeeByEmailInput],
    found: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]],
    not_found: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    ambiguous: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FindEmployeeByEmailOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, FindEmployeeByEmailInput, FindEmployeeByEmailOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "found": found,
    }
    if not_found is not None:
        branches["notFound"] = not_found
    if ambiguous is not None:
        branches["ambiguous"] = ambiguous
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
            operation=_bound_operation(connection, "find_employee_by_email"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_find_employee_by_email_step, '__module__', 'dex_connectors.bamboohr.steps')

def new_list_employee_changes_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListEmployeeChangesInput],
    listed: dex.StepTarget[QueryResult[ListEmployeeChangesOutput]],
    provider_rejected: dex.StepTarget[QueryResult[ListEmployeeChangesOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListEmployeeChangesOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListEmployeeChangesOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListEmployeeChangesOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListEmployeeChangesInput, ListEmployeeChangesOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_employee_changes"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_employee_changes_step, '__module__', 'dex_connectors.bamboohr.steps')

def new_list_time_off_requests_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListTimeOffRequestsInput],
    listed: dex.StepTarget[QueryResult[ListTimeOffRequestsOutput]],
    provider_rejected: dex.StepTarget[QueryResult[ListTimeOffRequestsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListTimeOffRequestsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListTimeOffRequestsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListTimeOffRequestsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListTimeOffRequestsInput, ListTimeOffRequestsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_time_off_requests"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_time_off_requests_step, '__module__', 'dex_connectors.bamboohr.steps')

def new_update_employee_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateEmployeeInput],
    updated: dex.StepTarget[MutationResult[UpdateEmployeeOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateEmployeeOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateEmployeeOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateEmployeeOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateEmployeeOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateEmployeeOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateEmployeeInput, UpdateEmployeeOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
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
            operation=_bound_operation(connection, "update_employee"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_employee_step, '__module__', 'dex_connectors.bamboohr.steps')

def new_add_employee_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddEmployeeInput],
    created: dex.StepTarget[MutationResult[AddEmployeeOutput]],
    provider_rejected: dex.StepTarget[MutationResult[AddEmployeeOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddEmployeeOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddEmployeeOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddEmployeeOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddEmployeeInput, AddEmployeeOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("BambooHR Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("BambooHR Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("BambooHR Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
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
            operation=_bound_operation(connection, "add_employee"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_employee_step, '__module__', 'dex_connectors.bamboohr.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
