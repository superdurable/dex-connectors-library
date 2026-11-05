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
from ..models import Form, FormResponse, GetFormInput, GetResponseInput, ListResponsesInput, ResponsePage

StepInput = TypeVar("StepInput")

def new_get_form_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetFormInput],
    found: dex.StepTarget[QueryResult[Form]],
    not_found: dex.StepTarget[QueryResult[Form]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Form]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Form]] | None = None,
    defect: dex.StepTarget[QueryResult[Form]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Form]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetFormInput, Form]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Forms Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Forms Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Forms Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_form"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_form_step, '__module__', 'dex_connectors.google_forms.steps')

def new_list_responses_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListResponsesInput],
    listed: dex.StepTarget[QueryResult[ResponsePage]],
    not_found: dex.StepTarget[QueryResult[ResponsePage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ResponsePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ResponsePage]] | None = None,
    defect: dex.StepTarget[QueryResult[ResponsePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ResponsePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListResponsesInput, ResponsePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Forms Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Forms Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Forms Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_responses"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_responses_step, '__module__', 'dex_connectors.google_forms.steps')

def new_get_response_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetResponseInput],
    found: dex.StepTarget[QueryResult[FormResponse]],
    not_found: dex.StepTarget[QueryResult[FormResponse]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[FormResponse]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[FormResponse]] | None = None,
    defect: dex.StepTarget[QueryResult[FormResponse]] | None = None,
    result_attribute: dex.Attribute[QueryResult[FormResponse]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetResponseInput, FormResponse]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Google Forms Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Google Forms Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Google Forms Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_response"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_response_step, '__module__', 'dex_connectors.google_forms.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
