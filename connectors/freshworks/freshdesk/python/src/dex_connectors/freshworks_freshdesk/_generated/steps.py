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
from ..models import AddNoteInput, AddNoteOutput, CreateTicketInput, CreateTicketOutput, GetTicketInput, SearchTicketsInput, SearchTicketsOutput, TicketDetails, UpdateTicketInput, UpdateTicketOutput

StepInput = TypeVar("StepInput")

def new_search_tickets_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], SearchTicketsInput],
    searched: dex.StepTarget[QueryResult[SearchTicketsOutput]],
    provider_rejected: dex.StepTarget[QueryResult[SearchTicketsOutput]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[SearchTicketsOutput]] | None = None,
    defect: dex.StepTarget[QueryResult[SearchTicketsOutput]] | None = None,
    result_attribute: dex.Attribute[QueryResult[SearchTicketsOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, SearchTicketsInput, SearchTicketsOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Freshdesk Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Freshdesk Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Freshdesk Step annotations must be strings")
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
            operation=_bound_operation(connection, "search_tickets"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_search_tickets_step, '__module__', 'dex_connectors.freshworks_freshdesk.steps')

def new_get_ticket_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetTicketInput],
    found: dex.StepTarget[QueryResult[TicketDetails]],
    not_found: dex.StepTarget[QueryResult[TicketDetails]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[TicketDetails]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[TicketDetails]] | None = None,
    defect: dex.StepTarget[QueryResult[TicketDetails]] | None = None,
    result_attribute: dex.Attribute[QueryResult[TicketDetails]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetTicketInput, TicketDetails]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Freshdesk Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Freshdesk Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Freshdesk Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_ticket"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_ticket_step, '__module__', 'dex_connectors.freshworks_freshdesk.steps')

def new_create_ticket_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateTicketInput],
    created: dex.StepTarget[MutationResult[CreateTicketOutput]],
    provider_rejected: dex.StepTarget[MutationResult[CreateTicketOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[CreateTicketOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[CreateTicketOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[CreateTicketOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateTicketInput, CreateTicketOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Freshdesk Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Freshdesk Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Freshdesk Step annotations must be strings")
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
            operation=_bound_operation(connection, "create_ticket"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_ticket_step, '__module__', 'dex_connectors.freshworks_freshdesk.steps')

def new_update_ticket_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateTicketInput],
    updated: dex.StepTarget[MutationResult[UpdateTicketOutput]],
    not_found: dex.StepTarget[MutationResult[UpdateTicketOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdateTicketOutput]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[UpdateTicketOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdateTicketOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdateTicketOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateTicketInput, UpdateTicketOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Freshdesk Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Freshdesk Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Freshdesk Step annotations must be strings")
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
            operation=_bound_operation(connection, "update_ticket"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_ticket_step, '__module__', 'dex_connectors.freshworks_freshdesk.steps')

def new_add_note_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], AddNoteInput],
    added: dex.StepTarget[MutationResult[AddNoteOutput]],
    not_found: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    uncertain: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    defect: dex.StepTarget[MutationResult[AddNoteOutput]] | None = None,
    result_attribute: dex.Attribute[MutationResult[AddNoteOutput]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, AddNoteInput, AddNoteOutput]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Freshdesk Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Freshdesk Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Freshdesk Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "added": added,
    }
    if not_found is not None:
        branches["notFound"] = not_found
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
            operation=_bound_operation(connection, "add_note"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_add_note_step, '__module__', 'dex_connectors.freshworks_freshdesk.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
