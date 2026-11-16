# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the eventChanged example's mapping, validation, and static binding."""

from datetime import UTC, datetime

import dex
from dex_connectors.sdk import TriggerEvent

from dex_connectors.google_calendar import Event, EventChangedEvent, GetEventInput
from dex_connectors.google_calendar.calendarmock import Mock
from examples.event_changed.flow import (
    CONNECTION_NAME,
    EVENT_CHANGED_TRIGGER_BINDING,
    FLOW_TYPE,
    EventChangeFlow,
    EventChangeInput,
    _invalid_input_reason,
    accept_every_event_change,
    map_to_flow_input,
    map_to_get_event_input,
    resolve_flow_id,
)

CHANGE = EventChangedEvent(
    calendar_id="team@group.calendar.google.com", change_kind="updated", event=Event("standup0001")
)
TRIGGER_EVENT = TriggerEvent("gcal-0123", datetime(2026, 3, 2, 9, 4, tzinfo=UTC), CHANGE)


def test_each_change_names_its_flow_and_reads_its_event_back() -> None:
    assert resolve_flow_id(TRIGGER_EVENT) == "google-calendar-event-change-gcal-0123"
    assert accept_every_event_change(TRIGGER_EVENT)
    flow_input = map_to_flow_input(TRIGGER_EVENT)
    assert flow_input == EventChangeInput("gcal-0123", CHANGE)
    assert map_to_get_event_input(flow_input) == GetEventInput(
        "team@group.calendar.google.com", "standup0001"
    )


def test_record_step_refuses_incomplete_changes() -> None:
    assert _invalid_input_reason(EventChangeInput("gcal-0123", CHANGE)) == ""
    assert _invalid_input_reason(EventChangeInput(" ", CHANGE)) == "triggerEventId is required"
    missing_event = EventChangedEvent(calendar_id="primary", change_kind="created")
    assert "change.event.id" in _invalid_input_reason(EventChangeInput("gcal-1", missing_event))
    unknown_kind = EventChangedEvent(calendar_id="primary", change_kind="moved", event=Event("e1"))
    assert "changeKind" in _invalid_input_reason(EventChangeInput("gcal-1", unknown_kind))


def test_flow_declares_the_static_binding_with_the_calendar_picker() -> None:
    flow = EventChangeFlow(Mock(CONNECTION_NAME).connection())
    dex.Registry([flow], allow_async_handlers=True)
    assert flow.get_flow_type() == FLOW_TYPE
    [binding] = flow.get_connector_trigger_bindings()
    assert binding.definition.trigger.trigger_name == "eventChanged"
    assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, EVENT_CHANGED_TRIGGER_BINDING)
    assert binding.configuration_ui is not None
    [unit] = binding.configuration_ui.units
    assert (unit.id, unit.unit_id, unit.label, unit.required) == (
        "watchedCalendar",
        "calendarPicker",
        "Watched calendar",
        False,
    )
    assert [(item.port, item.json_pointer) for item in unit.bindings] == [
        ("calendarId", "/calendarId"),
        ("calendarName", "/calendarName"),
    ]
    assert "primary calendar" in unit.description and "baseline" in unit.description
