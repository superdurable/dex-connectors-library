# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Deterministic checks of the meeting_changed example's Flow wiring and mappers."""

from datetime import UTC, datetime

import dex
import httpx
import pytest
from dex_connectors.sdk import ConnectionRef, StaticCredentialProvider, TriggerEvent

from dex_connectors.zoom import Client, Config, MeetingChangedEvent, MeetingSummary, new_connection
from examples.meeting_changed.flow import (
    BINDING_NAME,
    CONNECTION_NAME,
    MeetingChangeFlow,
    MeetingChangeInput,
    accepts_every_meeting_change,
    map_to_flow_input,
    map_to_get_meeting_input,
    resolve_flow_id,
    validate_meeting_change_input,
)


def change(kind: str = "updated", meeting_id: int = 92674392836) -> TriggerEvent[MeetingChangedEvent]:
    """One delivered change."""
    payload = MeetingChangedEvent(
        change_kind=kind, meeting=MeetingSummary(id=meeting_id, topic="Consultation")
    )
    return TriggerEvent("zoom-meeting-0123", datetime(2036, 2, 23, tzinfo=UTC), payload)


def test_mappers_name_one_flow_per_change_and_read_the_changed_meeting() -> None:
    event = change()
    assert resolve_flow_id(event) == "zoom-meeting-change-zoom-meeting-0123"
    flow_input = map_to_flow_input(event)
    assert flow_input == MeetingChangeInput("zoom-meeting-0123", event.payload)
    assert map_to_get_meeting_input(flow_input).meeting_id == 92674392836
    assert accepts_every_meeting_change(change("removed"))


def test_validation_refuses_an_unusable_change() -> None:
    assert validate_meeting_change_input(map_to_flow_input(change())) == ""
    assert "triggerEventId" in validate_meeting_change_input(MeetingChangeInput(" ", change().payload))
    assert "meeting.id" in validate_meeting_change_input(map_to_flow_input(change(meeting_id=0)))
    assert "changeKind" in validate_meeting_change_input(map_to_flow_input(change(kind="ended")))


async def test_flow_registers_and_declares_its_trigger_binding() -> None:
    async with httpx.AsyncClient() as http_client:
        client = Client(Config(), StaticCredentialProvider({}), http_client)
        flow = MeetingChangeFlow(new_connection(client, ConnectionRef("zoom", CONNECTION_NAME)))
        dex.Registry([flow], allow_async_handlers=True)
        assert flow.get_flow_type() == "ZoomMeetingChangePython"
        [binding] = flow.get_connector_trigger_bindings()
        assert (binding.connection_name, binding.binding_name) == (CONNECTION_NAME, BINDING_NAME)
        assert binding.definition.trigger.trigger_name == "meetingChanged"
        with pytest.raises(ValueError, match=CONNECTION_NAME):
            MeetingChangeFlow(new_connection(client, ConnectionRef("zoom", "other")))
