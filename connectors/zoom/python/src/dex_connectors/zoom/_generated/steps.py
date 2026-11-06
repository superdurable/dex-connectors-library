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
from ..models import CreateMeetingInput, GetMeetingInput, ListMeetingsInput, ListPastMeetingParticipantsInput, Meeting, MeetingPage, ParticipantPage, UpdateMeetingInput, UpdatedMeeting

StepInput = TypeVar("StepInput")

def new_list_meetings_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListMeetingsInput],
    listed: dex.StepTarget[QueryResult[MeetingPage]],
    provider_rejected: dex.StepTarget[QueryResult[MeetingPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[MeetingPage]] | None = None,
    defect: dex.StepTarget[QueryResult[MeetingPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[MeetingPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListMeetingsInput, MeetingPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Zoom Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Zoom Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Zoom Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_meetings"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_meetings_step, '__module__', 'dex_connectors.zoom.steps')

def new_get_meeting_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetMeetingInput],
    found: dex.StepTarget[QueryResult[Meeting]],
    not_found: dex.StepTarget[QueryResult[Meeting]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[Meeting]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[Meeting]] | None = None,
    defect: dex.StepTarget[QueryResult[Meeting]] | None = None,
    result_attribute: dex.Attribute[QueryResult[Meeting]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetMeetingInput, Meeting]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Zoom Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Zoom Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Zoom Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_meeting"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_meeting_step, '__module__', 'dex_connectors.zoom.steps')

def new_create_meeting_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateMeetingInput],
    created: dex.StepTarget[MutationResult[Meeting]],
    provider_rejected: dex.StepTarget[MutationResult[Meeting]] | None = None,
    uncertain: dex.StepTarget[MutationResult[Meeting]] | None = None,
    defect: dex.StepTarget[MutationResult[Meeting]] | None = None,
    result_attribute: dex.Attribute[MutationResult[Meeting]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateMeetingInput, Meeting]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Zoom Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Zoom Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Zoom Step annotations must be strings")
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
            operation=_bound_operation(connection, "create_meeting"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_meeting_step, '__module__', 'dex_connectors.zoom.steps')

def new_update_meeting_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], UpdateMeetingInput],
    updated: dex.StepTarget[MutationResult[UpdatedMeeting]],
    not_found: dex.StepTarget[MutationResult[UpdatedMeeting]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[UpdatedMeeting]] | None = None,
    defect: dex.StepTarget[MutationResult[UpdatedMeeting]] | None = None,
    result_attribute: dex.Attribute[MutationResult[UpdatedMeeting]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, UpdateMeetingInput, UpdatedMeeting]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Zoom Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Zoom Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Zoom Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "updated": updated,
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
            operation=_bound_operation(connection, "update_meeting"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_update_meeting_step, '__module__', 'dex_connectors.zoom.steps')

def new_list_past_meeting_participants_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListPastMeetingParticipantsInput],
    listed: dex.StepTarget[QueryResult[ParticipantPage]],
    not_found: dex.StepTarget[QueryResult[ParticipantPage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ParticipantPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ParticipantPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ParticipantPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ParticipantPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListPastMeetingParticipantsInput, ParticipantPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Zoom Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Zoom Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Zoom Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_past_meeting_participants"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_past_meeting_participants_step, '__module__', 'dex_connectors.zoom.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
