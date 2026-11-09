# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable PostHog Flow capturing one deduplicated event."""

from dataclasses import dataclass, field, replace

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.posthog import CapturedEvent, CaptureEventInput, Connection, new_capture_event_step

FLOW_TYPE = "PostHogPythonEventCapture"
CONNECTION_NAME = "posthog"
_CAPTURE_STEP_TYPE = "CapturePostHogEvent"
_OUTCOME = dex.Attribute("posthog-python-event-capture-outcome", CapturedEvent)


@dataclass(frozen=True, slots=True)
class EventCaptureRequest:
    """One stable event; timestamp is an RFC3339 UTC instant across retries."""

    event: str
    distinct_id: str = field(metadata={"dex_json_name": "distinctId"})
    timestamp: str = ""


def map_to_capture_event_input(request: EventCaptureRequest) -> CaptureEventInput:
    """Select the provider-safe fields for one event capture."""
    return CaptureEventInput(request.event, request.distinct_id, request.timestamp)


# dex:group group-id:event-capture group-label:"Event capture"
# dex:explanation text:"Fix a stable timestamp and validate event identity before sending to PostHog."
class _Start(dex.Step[EventCaptureRequest]):
    def get_step_type(self) -> str:
        return "StartPostHogEventCapture"

    def execute(self, context: dex.Context, input: EventCaptureRequest) -> dex.StepDecision:
        if not input.event.strip() or not input.distinct_id.strip():
            return dex.force_fail("event and distinctId are required")
        timestamp = input.timestamp or context.flow_started_at.isoformat().replace("+00:00", "Z")
        prepared = replace(
            input, event=input.event.strip(), distinct_id=input.distinct_id.strip(), timestamp=timestamp
        )
        return dex.go_to(dex.StepRef("CapturePostHogEvent"), prepared)


# dex:group group-id:event-capture group-label:"Event capture"
# dex:explanation text:"Complete after PostHog accepts the event for processing."
class _Accepted(dex.Step[MutationResult[CapturedEvent]]):
    def get_step_type(self) -> str:
        return "PostHogEventAccepted"

    def execute(self, context: dex.Context, input: MutationResult[CapturedEvent]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class EventCaptureFlow(dex.Flow[EventCaptureRequest]):
    """Capture one event through the static posthog connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("event capture requires the static posthog connection")
        self._start = _Start()
        self._accepted = _Accepted()
        self._capture = new_capture_event_step(
            step_type=_CAPTURE_STEP_TYPE,
            step_input_type=EventCaptureRequest,
            annotations={
                "groupId": "event-capture",
                "groupLabel": "Event capture",
                "explanation": "Submit one PostHog event with a stable timestamp and UUID.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_capture_event_input,
            accepted=self._accepted,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return "PostHogPythonEventCapture"

    def get_steps(self) -> dex.StepList[EventCaptureRequest]:
        """Register validation, capture, and confirmation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._capture, self._accepted)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist only the accepted event identity."""
        return dex.PersistenceSchema.of(_OUTCOME)
