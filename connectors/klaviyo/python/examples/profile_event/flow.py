# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Klaviyo Flow that submits one deduplicated profile event."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.klaviyo import Connection, CreateEventInput, EventAcceptance, new_create_event_step

FLOW_TYPE = "KlaviyoProfileEvent"
CONNECTION_NAME = "klaviyo"
_EVENT_STEP_TYPE = "CreateKlaviyoEvent"
_OUTCOME = dex.Attribute("klaviyo-profile-event-outcome", EventAcceptance)


@dataclass(frozen=True, slots=True)
class ProfileEventRequest:
    """Profile email and the metric name of the event to record."""

    email: str
    metric_name: str = field(metadata={"dex_json_name": "metricName"})


def map_to_create_event_input(request: ProfileEventRequest) -> CreateEventInput:
    """Submit the event for the requested profile email."""
    return CreateEventInput(email=request.email, metric_name=request.metric_name)


# dex:group group-id:profile-event group-label:"Profile event"
# dex:explanation text:"Trim and check the email and metric name before submitting the event."
class _Start(dex.Step[ProfileEventRequest]):
    def get_step_type(self) -> str:
        return "StartKlaviyoProfileEvent"

    def execute(self, context: dex.Context, input: ProfileEventRequest) -> dex.StepDecision:
        request = ProfileEventRequest(input.email.strip(), input.metric_name.strip())
        if not request.email or not request.metric_name:
            return dex.force_fail("email and metricName are required")
        return dex.go_to(dex.StepRef(_EVENT_STEP_TYPE), request)


# dex:group group-id:profile-event group-label:"Profile event"
# dex:explanation text:"Record Klaviyo's acceptance and complete the Flow."
class _Accepted(dex.Step[MutationResult[EventAcceptance]]):
    def get_step_type(self) -> str:
        return "KlaviyoEventAccepted"

    def execute(self, context: dex.Context, input: MutationResult[EventAcceptance]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class ProfileEventFlow(dex.Flow[ProfileEventRequest]):
    """Submit one Klaviyo event through the static klaviyo connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("profile event requires the static klaviyo connection")
        self._start = _Start()
        self._accepted = _Accepted()
        self._event = new_create_event_step(
            step_type=_EVENT_STEP_TYPE,
            step_input_type=ProfileEventRequest,
            annotations={
                "groupId": "profile-event",
                "groupLabel": "Profile event",
                "explanation": "Submit one event for a Klaviyo profile with stable deduplication.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_create_event_input,
            accepted=self._accepted,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[ProfileEventRequest]:
        """Register the start, event submission, and acceptance Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._event, self._accepted)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist Klaviyo's acceptance."""
        return dex.PersistenceSchema.of(_OUTCOME)
