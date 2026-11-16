# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""One Flow per Zoom meeting change: record it and read a scheduled or updated meeting back."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import QueryResult, TriggerBindingDefinition, TriggerEvent

from dex_connectors.zoom import (
    Connection,
    GetMeetingInput,
    Meeting,
    MeetingChangedEvent,
    MeetingChangeKind,
    define_meeting_changed_trigger_binding,
    new_get_meeting_step,
)

CONNECTION_NAME = "zoom-meeting-watcher"
BINDING_NAME = "host-meeting-changes"
FLOW_TYPE = "ZoomMeetingChangePython"


@dataclass(frozen=True, slots=True)
class MeetingChangeInput:
    """One meetingChanged delivery: the stable Trigger event ID beside the delivered change."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: MeetingChangedEvent


@dataclass(frozen=True, slots=True)
class MeetingChangeOutcome:
    """The Flow result: the changed meeting and, unless it is gone, its state read back from Zoom."""

    meeting_id: int = field(metadata={"dex_json_name": "meetingId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: Meeting | None = field(default=None, metadata={"dex_json_omit_default": True})


meeting_change_attribute = dex.Attribute("zoom-meeting-change", MeetingChangeInput)
meeting_change_outcome_attribute = dex.Attribute("zoom-meeting-change-outcome", MeetingChangeOutcome)


def resolve_flow_id(event: TriggerEvent[MeetingChangedEvent]) -> str:
    """Name one Flow per change, so a redelivered change finds its existing Flow."""
    return f"zoom-meeting-change-{event.id}"


def map_to_flow_input(event: TriggerEvent[MeetingChangedEvent]) -> MeetingChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return MeetingChangeInput(event.id, event.payload)


def accepts_every_meeting_change(event: TriggerEvent[MeetingChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to reschedules."""
    return True


def map_to_get_meeting_input(input: MeetingChangeInput) -> GetMeetingInput:
    """Read the changed meeting by its ID."""
    return GetMeetingInput(meeting_id=input.change.meeting.id)


def validate_meeting_change_input(input: MeetingChangeInput) -> str:
    """Return why a delivered change is unusable, or "" when it is valid."""
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if input.change.meeting.id < 1:
        return "change.meeting.id is required"
    if input.change.change_kind not in tuple(MeetingChangeKind):
        return "change.changeKind must be scheduled, updated, or removed"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the meeting read back, or none when deleted before the read, and complete."
class CompleteMeetingChange(dex.Step[QueryResult[Meeting]]):
    """Record the read-back outcome and complete."""

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "CompleteMeetingChange"

    def execute(self, context: dex.Context, input: QueryResult[Meeting]) -> dex.StepDecision:
        """Keep the current meeting only when Zoom found it."""
        change = meeting_change_attribute.get(context)
        outcome = MeetingChangeOutcome(
            change.change.meeting.id,
            change.change.change_kind,
            input.value if input.branch == "found" else None,
        )
        meeting_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the change; complete at once for a removed meeting, otherwise read it back."
class RecordMeetingChange(dex.Step[MeetingChangeInput]):
    """Validate and record the delivered change."""

    def __init__(self, read_changed_meeting: dex.StepTarget[MeetingChangeInput]) -> None:
        """Bind the read-back Step this Step continues to."""
        self.read_changed_meeting = read_changed_meeting

    def get_step_type(self) -> str:
        """Return the stable Step identity."""
        return "RecordMeetingChange"

    def execute(self, context: dex.Context, input: MeetingChangeInput) -> dex.StepDecision:
        """Complete at once for a removal, which Zoom no longer lists; read anything else back."""
        problem = validate_meeting_change_input(input)
        if problem:
            return dex.force_fail(problem)
        meeting_change_attribute.set(context, input)
        if input.change.change_kind == MeetingChangeKind.REMOVED:
            outcome = MeetingChangeOutcome(input.change.meeting.id, input.change.change_kind)
            meeting_change_outcome_attribute.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(self.read_changed_meeting, input)


class MeetingChangeFlow(dex.Flow[MeetingChangeInput]):
    """Record one Zoom meeting change and read a scheduled or updated meeting back."""

    def __init__(self, connection: Connection) -> None:
        """Bind the static Zoom connection at Worker registration."""
        if connection.reference.name != CONNECTION_NAME:
            raise ValueError(f"meeting change Flow requires the {CONNECTION_NAME} connection")
        self.complete = CompleteMeetingChange()
        self.read_changed_meeting = new_get_meeting_step(
            step_type="ReadChangedMeeting",
            step_input_type=MeetingChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed meeting back, so the record holds its full agenda and "
                "settings rather than the listed summary.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_meeting_input,
            found=self.complete,
            # Business reaction: a meeting deleted after the poll is recorded without a current state.
            not_found=self.complete,
        )
        self.record = RecordMeetingChange(self.read_changed_meeting)

    def get_flow_type(self) -> str:
        """Return the stable Flow identity."""
        return "ZoomMeetingChangePython"

    def get_steps(self) -> dex.StepList[MeetingChangeInput]:
        """Start at the record Step, which the Trigger starts the Flow at."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_meeting, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(meeting_change_attribute, meeting_change_outcome_attribute)

    # Dex Web shows no field for this binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_meeting_changed_trigger_binding(
                connection_name="zoom-meeting-watcher", binding_name="host-meeting-changes"
            )
        ]
