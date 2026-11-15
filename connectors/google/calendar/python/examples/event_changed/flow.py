# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Start one Flow per Google Calendar event change, read the event back, and record its state."""

from dataclasses import dataclass, field

import dex
from dex_connectors.sdk import (
    ConnectorConfigurationUI,
    ConnectorUIBinding,
    ConnectorUIUnit,
    QueryResult,
    TriggerBindingDefinition,
    TriggerEvent,
)

from dex_connectors.google_calendar import (
    Connection,
    Event,
    EventChangedEvent,
    EventChangeKind,
    GetEventInput,
    define_event_changed_trigger_binding,
    new_get_event_step,
)

FLOW_TYPE = "GoogleCalendarEventChangePython"
CONNECTION_NAME = "google-calendar-watcher"
EVENT_CHANGED_TRIGGER_BINDING = "calendar-event-changes"
READ_CHANGED_EVENT_STEP_TYPE = "ReadChangedEvent"


@dataclass(frozen=True, slots=True)
class EventChangeInput:
    """One eventChanged delivery with the Trigger event ID that also names the Flow."""

    trigger_event_id: str = field(metadata={"dex_json_name": "triggerEventId"})
    change: EventChangedEvent = field(metadata={"dex_json_name": "change"})


@dataclass(frozen=True, slots=True)
class ChangeOutcome:
    """The Flow result: the change and, unless it was cancelled, the event read back from Google."""

    calendar_id: str = field(metadata={"dex_json_name": "calendarId"})
    event_id: str = field(metadata={"dex_json_name": "eventId"})
    change_kind: str = field(metadata={"dex_json_name": "changeKind"})
    current: Event | None = field(
        default=None, metadata={"dex_json_name": "current", "dex_json_omit_default": True}
    )


event_change_attribute = dex.Attribute("google-calendar-event-change", EventChangeInput)
event_change_outcome_attribute = dex.Attribute("google-calendar-event-change-outcome", ChangeOutcome)


def resolve_flow_id(event: TriggerEvent[EventChangedEvent]) -> str:
    """Name one Flow per change; a redelivered change finds its existing Flow."""
    return "google-calendar-event-change-" + event.id


def map_to_flow_input(event: TriggerEvent[EventChangedEvent]) -> EventChangeInput:
    """Keep the Trigger event ID beside the delivered change."""
    return EventChangeInput(event.id, event.payload)


def accept_every_event_change(event: TriggerEvent[EventChangedEvent]) -> bool:
    """Admit every change; an application narrows it, for example to cancellations."""
    return True


def map_to_get_event_input(input: EventChangeInput) -> GetEventInput:
    """Read the changed event from its calendar."""
    return GetEventInput(input.change.calendar_id, input.change.event.id)


def _invalid_input_reason(input: EventChangeInput) -> str:
    if not input.trigger_event_id.strip():
        return "triggerEventId is required"
    if not input.change.calendar_id.strip() or not input.change.event.id.strip():
        return "change.calendarId and change.event.id are required"
    if input.change.change_kind not in {kind.value for kind in EventChangeKind}:
        return "change.changeKind must be created, updated, or cancelled"
    return ""


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the change; complete at once for a cancellation, else read the event back."
class RecordEventChange(dex.Step[EventChangeInput]):
    """Validate and persist the change, then complete or read the event back."""

    def __init__(self, read_changed_event: dex.StepTarget[EventChangeInput]) -> None:
        """Bind the connector Step that reads a changed event."""
        self._read_changed_event = read_changed_event

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "RecordEventChange"

    def execute(self, context: dex.Context, input: EventChangeInput) -> dex.StepDecision:
        """Complete a cancellation without a read; read any other change back from Google."""
        reason = _invalid_input_reason(input)
        if reason:
            return dex.force_fail(reason)
        event_change_attribute.set(context, input)
        if input.change.change_kind == EventChangeKind.CANCELLED.value:
            outcome = ChangeOutcome(input.change.calendar_id, input.change.event.id, input.change.change_kind)
            event_change_outcome_attribute.set(context, outcome)
            return dex.graceful_complete(outcome)
        return dex.go_to(self._read_changed_event, input)


# dex:group group-id:change group-label:"Change"
# dex:explanation text:"Record the event read back from Google Calendar as the outcome and complete the Flow."
class CompleteEventChange(dex.Step[QueryResult[Event]]):
    """Record the current event as the outcome."""

    def get_step_type(self) -> str:
        """Keep the Step identity stable."""
        return "CompleteEventChange"

    def execute(self, context: dex.Context, input: QueryResult[Event]) -> dex.StepDecision:
        """Complete the Flow with the change and the current event."""
        change = event_change_attribute.get(context)
        outcome = ChangeOutcome(
            change.change.calendar_id, change.change.event.id, change.change.change_kind, input.value
        )
        event_change_outcome_attribute.set(context, outcome)
        return dex.graceful_complete(outcome)


class EventChangeFlow(dex.Flow[EventChangeInput]):
    """Record one Google Calendar event change started by the eventChanged Trigger."""

    def __init__(self, connection: Connection) -> None:
        """Bind the Google Calendar connection before Worker registration."""
        self.complete = CompleteEventChange()
        self.read_changed_event = new_get_event_step(
            step_type=READ_CHANGED_EVENT_STEP_TYPE,
            step_input_type=EventChangeInput,
            annotations={
                "groupId": "change",
                "groupLabel": "Change",
                "explanation": "Read the changed event back, so the record holds its current state rather "
                "than the sync snapshot.",
            },
            connection=connection,
            connection_name=CONNECTION_NAME,
            map_to_operation_input=map_to_get_event_input,
            found=self.complete,
        )
        self.record = RecordEventChange(self.read_changed_event)

    def get_flow_type(self) -> str:
        """Keep the application Flow identity stable across Worker versions."""
        return "GoogleCalendarEventChangePython"

    def get_steps(self) -> dex.StepList[EventChangeInput]:
        """Record the change, read it back, and complete."""
        return dex.StepList.start_step(self.record).other_steps(self.read_changed_event, self.complete)

    def get_persistence_schema(self) -> dex.PersistenceSchema:
        """Register the change and outcome Attributes."""
        return dex.PersistenceSchema.of(event_change_attribute, event_change_outcome_attribute)

    # Dex Web configures this static binding; the analyzer requires a lone return statement.
    def get_connector_trigger_bindings(self) -> list[TriggerBindingDefinition]:
        return [
            define_event_changed_trigger_binding(
                connection_name="google-calendar-watcher",
                binding_name="calendar-event-changes",
                configuration_ui=ConnectorConfigurationUI(
                    units=(
                        ConnectorUIUnit(
                            id="watchedCalendar",
                            unit_id="calendarPicker",
                            label="Watched calendar",
                            description="Choose the calendar whose created, updated, and cancelled events start this Flow; the list comes from this connection's Google Calendar list and saves the calendar ID and name. Leave it unsaved to watch the authorized account's primary calendar. The first start records existing events as a baseline and starts no Flow for them. Restart the Worker after saving; a changed calendar records a new baseline.",  # noqa: E501
                            required=False,
                            bindings=(
                                ConnectorUIBinding(port="calendarId", json_pointer="/calendarId"),
                                ConnectorUIBinding(port="calendarName", json_pointer="/calendarName"),
                            ),
                        ),
                    )
                ),
            )
        ]
