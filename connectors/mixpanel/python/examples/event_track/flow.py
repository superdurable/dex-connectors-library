# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Runnable Mixpanel Flow that imports one deduplicated event."""

from dataclasses import dataclass, field, replace
from datetime import datetime

import dex
from dex_connectors.sdk import MutationResult

from dex_connectors.mixpanel import Connection, EventImportResult, TrackEventInput, new_track_event_step

FLOW_TYPE = "MixpanelEventTrack"
CONNECTION_NAME = "mixpanel"
_TRACK_STEP_TYPE = "ImportMixpanelEvent"
_OUTCOME = dex.Attribute("mixpanel-event-track-outcome", EventImportResult)


@dataclass(frozen=True, slots=True)
class EventTrackRequest:
    """Event name, distinct ID, and Unix time in seconds; zero time fixes the Flow start time."""

    event: str
    distinct_id: str = field(metadata={"dex_json_name": "distinctID"})
    time: int = field(default=0, metadata={"dex_json_omit_default": True})


def prepare_event_track_request(request: EventTrackRequest, now: datetime) -> EventTrackRequest:
    """Trim event identity and fix a missing timestamp so retries reuse it."""
    prepared = replace(request, event=request.event.strip(), distinct_id=request.distinct_id.strip())
    if not prepared.event or not prepared.distinct_id:
        raise ValueError("event and distinctID are required")
    if prepared.time == 0:
        prepared = replace(prepared, time=int(now.timestamp()))
    return prepared


def map_to_track_event_input(request: EventTrackRequest) -> TrackEventInput:
    """Import the prepared event with its stable deduplication fields."""
    return TrackEventInput(event=request.event, distinct_id=request.distinct_id, time=request.time)


# dex:group group-id:event-track group-label:"Event track"
# dex:explanation text:"Validate event identity and fix its timestamp before calling Mixpanel."
class _Start(dex.Step[EventTrackRequest]):
    def get_step_type(self) -> str:
        return "StartMixpanelEventTrack"

    def execute(self, context: dex.Context, input: EventTrackRequest) -> dex.StepDecision:
        try:
            prepared = prepare_event_track_request(input, context.flow_started_at)
        except ValueError as error:
            return dex.force_fail(str(error))
        return dex.go_to(dex.StepRef(_TRACK_STEP_TYPE), prepared)


# dex:group group-id:event-track group-label:"Event track"
# dex:explanation text:"Complete after Mixpanel confirms event ingestion."
class _Imported(dex.Step[MutationResult[EventImportResult]]):
    def get_step_type(self) -> str:
        return "MixpanelEventImported"

    def execute(self, context: dex.Context, input: MutationResult[EventImportResult]) -> dex.StepDecision:
        _OUTCOME.set(context, input.value)
        return dex.graceful_complete(input.value)


class EventTrackFlow(dex.Flow[EventTrackRequest]):
    """Import one Mixpanel event through the static mixpanel connection."""

    def __init__(self, connection: Connection) -> None:
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError("event track requires the static mixpanel connection")
        self._start = _Start()
        self._imported = _Imported()
        self._track = new_track_event_step(
            step_type=_TRACK_STEP_TYPE,
            step_input_type=EventTrackRequest,
            annotations={
                "groupId": "event-track",
                "groupLabel": "Event track",
                "explanation": "Import one event with stable time and distinct ID for deduplication.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_track_event_input,
            imported=self._imported,
        )

    def get_flow_type(self) -> str:
        """Return the stable Flow identity used in Dex Web."""
        return FLOW_TYPE

    def get_steps(self) -> dex.StepList[EventTrackRequest]:
        """Register the start, event import, and confirmation Steps."""
        return dex.StepList.start_step(self._start).other_steps(self._track, self._imported)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Persist Mixpanel's import confirmation."""
        return dex.PersistenceSchema.of(_OUTCOME)
