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
from ..models import CancelScheduledEventInput, CreateSchedulingLinkInput, CreateWebhookSubscriptionInput, EventInviteePage, GetScheduledEventInput, ListEventInviteesInput, ListScheduledEventsInput, ScheduledEvent, ScheduledEventCancellation, ScheduledEventPage, SchedulingLink, WebhookSubscription

StepInput = TypeVar("StepInput")

def new_list_scheduled_events_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListScheduledEventsInput],
    listed: dex.StepTarget[QueryResult[ScheduledEventPage]],
    provider_rejected: dex.StepTarget[QueryResult[ScheduledEventPage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ScheduledEventPage]] | None = None,
    defect: dex.StepTarget[QueryResult[ScheduledEventPage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ScheduledEventPage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListScheduledEventsInput, ScheduledEventPage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_scheduled_events"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_scheduled_events_step, '__module__', 'dex_connectors.calendly.steps')

def new_get_scheduled_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], GetScheduledEventInput],
    found: dex.StepTarget[QueryResult[ScheduledEvent]],
    not_found: dex.StepTarget[QueryResult[ScheduledEvent]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[ScheduledEvent]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[ScheduledEvent]] | None = None,
    defect: dex.StepTarget[QueryResult[ScheduledEvent]] | None = None,
    result_attribute: dex.Attribute[QueryResult[ScheduledEvent]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, GetScheduledEventInput, ScheduledEvent]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
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
            operation=_bound_operation(connection, "get_scheduled_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_get_scheduled_event_step, '__module__', 'dex_connectors.calendly.steps')

def new_list_event_invitees_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], ListEventInviteesInput],
    listed: dex.StepTarget[QueryResult[EventInviteePage]],
    not_found: dex.StepTarget[QueryResult[EventInviteePage]] | None = None,
    provider_rejected: dex.StepTarget[QueryResult[EventInviteePage]] | None = None,
    invalid_response: dex.StepTarget[QueryResult[EventInviteePage]] | None = None,
    defect: dex.StepTarget[QueryResult[EventInviteePage]] | None = None,
    result_attribute: dex.Attribute[QueryResult[EventInviteePage]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> QueryStep[StepInput, ListEventInviteesInput, EventInviteePage]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
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
            operation=_bound_operation(connection, "list_event_invitees"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_list_event_invitees_step, '__module__', 'dex_connectors.calendly.steps')

def new_cancel_scheduled_event_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CancelScheduledEventInput],
    canceled: dex.StepTarget[MutationResult[ScheduledEventCancellation]],
    not_found: dex.StepTarget[MutationResult[ScheduledEventCancellation]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[ScheduledEventCancellation]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[ScheduledEventCancellation]] | None = None,
    defect: dex.StepTarget[MutationResult[ScheduledEventCancellation]] | None = None,
    result_attribute: dex.Attribute[MutationResult[ScheduledEventCancellation]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CancelScheduledEventInput, ScheduledEventCancellation]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "canceled": canceled,
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
            operation=_bound_operation(connection, "cancel_scheduled_event"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_cancel_scheduled_event_step, '__module__', 'dex_connectors.calendly.steps')

def new_create_scheduling_link_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateSchedulingLinkInput],
    created: dex.StepTarget[MutationResult[SchedulingLink]],
    provider_rejected: dex.StepTarget[MutationResult[SchedulingLink]] | None = None,
    uncertain: dex.StepTarget[MutationResult[SchedulingLink]] | None = None,
    defect: dex.StepTarget[MutationResult[SchedulingLink]] | None = None,
    result_attribute: dex.Attribute[MutationResult[SchedulingLink]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateSchedulingLinkInput, SchedulingLink]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
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
            operation=_bound_operation(connection, "create_scheduling_link"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_scheduling_link_step, '__module__', 'dex_connectors.calendly.steps')

def new_create_webhook_subscription_step(
    *,
    step_type: str,
    step_input_type: type[StepInput],
    annotations: StepAnnotations | Mapping[str, str],
    connection: Connection,
    connection_name: str,
    map_to_operation_input: Callable[[StepInput], CreateWebhookSubscriptionInput],
    subscribed: dex.StepTarget[MutationResult[WebhookSubscription]],
    conflicting_subscription: dex.StepTarget[MutationResult[WebhookSubscription]] | None = None,
    provider_rejected: dex.StepTarget[MutationResult[WebhookSubscription]] | None = None,
    invalid_response: dex.StepTarget[MutationResult[WebhookSubscription]] | None = None,
    defect: dex.StepTarget[MutationResult[WebhookSubscription]] | None = None,
    result_attribute: dex.Attribute[MutationResult[WebhookSubscription]] | None = None,
    step_options_override: dex.StepOptions | None = None,
) -> MutationStep[StepInput, CreateWebhookSubscriptionInput, WebhookSubscription]:
    if connection is None or connection.reference.name != connection_name:
        raise ValueError("Calendly Step connection name must match its connection reference")
    if isinstance(annotations, StepAnnotations):
        step_annotations = annotations
    else:
        if not isinstance(annotations, Mapping) or set(annotations) != {"groupId", "groupLabel", "explanation"}:
            raise ValueError("Calendly Step annotations require groupId, groupLabel, and explanation")
        if any(not isinstance(value, str) for value in annotations.values()):
            raise ValueError("Calendly Step annotations must be strings")
        step_annotations = StepAnnotations(annotations["groupId"], annotations["groupLabel"], annotations["explanation"])
    branches = {
        "subscribed": subscribed,
    }
    if conflicting_subscription is not None:
        branches["conflictingSubscription"] = conflicting_subscription
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
            operation=_bound_operation(connection, "create_webhook_subscription"),
            connection=connection.reference,
            map_to_operation_input=map_to_operation_input,
            branches=branches,
            result_attribute=result_attribute,
            step_options_override=step_options_override,
        )
    )

setattr(new_create_webhook_subscription_step, '__module__', 'dex_connectors.calendly.steps')


def _bound_operation(connection: Connection, operation_name: str) -> Any:
    source = getattr(connection, operation_name, None)
    if source is None:
        source = getattr(getattr(connection, "operations", None), operation_name, None)
    if source is None:
        raise ValueError(f"connection operation {operation_name} is missing")
    return source() if callable(source) else source
