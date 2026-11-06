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
from ..models import CreateEventInput, CreateEventOutput, Event, GetEventInput, ListEventsInput, ListEventsOutput, QueryFreeBusyInput, QueryFreeBusyOutput, UpdateEventInput, UpdateEventOutput

StepInput = TypeVar("StepInput")

def new_list_events_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListEventsInput],
    listed: dex.StepTarget[QueryResult[ListEventsOutput]],
    not_found: dex.StepTarget[QueryResult[ListEventsOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ListEventsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ListEventsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[ListEventsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ListEventsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListEventsInput, ListEventsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Outlook Calendar Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Outlook Calendar Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Outlook Calendar Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_events"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_events_step, '__module__', 'dex_connectors.microsoft_outlook_calendar.steps')

def new_get_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetEventInput],
    found: dex.StepTarget[QueryResult[Event]],
    not_found: dex.StepTarget[QueryResult[Event]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Event]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Event]] | None = None,
    defect: dex.StepTarget[QueryResult[Event]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Event]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetEventInput, Event]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Outlook Calendar Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Outlook Calendar Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Outlook Calendar Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_event_step, '__module__', 'dex_connectors.microsoft_outlook_calendar.steps')

def new_create_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateEventInput],
    created: dex.StepTarget[MutationResult[CreateEventOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateEventOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[CreateEventOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateEventOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateEventOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateEventInput, CreateEventOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Outlook Calendar Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Outlook Calendar Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Outlook Calendar Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "created": created,
    }
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
            operation=_bound_operation(connection, "create_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_event_step, '__module__', 'dex_connectors.microsoft_outlook_calendar.steps')

def new_update_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateEventInput],
    updated: dex.StepTarget[MutationResult[UpdateEventOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateEventOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateEventOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateEventOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateEventOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateEventOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateEventInput, UpdateEventOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Outlook Calendar Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Outlook Calendar Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Outlook Calendar Step annotations must be strings")
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
            operation=_bound_operation(connection, "update_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_event_step, '__module__', 'dex_connectors.microsoft_outlook_calendar.steps')

def new_query_free_busy_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], QueryFreeBusyInput],
    queried: dex.StepTarget[QueryResult[QueryFreeBusyOutput]],
    incomplete: dex.StepTarget[QueryResult[QueryFreeBusyOutput]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[QueryFreeBusyOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[QueryFreeBusyOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[QueryFreeBusyOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[QueryFreeBusyOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, QueryFreeBusyInput, QueryFreeBusyOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Microsoft Outlook Calendar Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Microsoft Outlook Calendar Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Microsoft Outlook Calendar Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "queried": queried,
    }
    if incomplete is not None:
        branches["incomplete"] = incomplete
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
            operation=_bound_operation(connection, "query_free_busy"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_query_free_busy_step, '__module__', 'dex_connectors.microsoft_outlook_calendar.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
