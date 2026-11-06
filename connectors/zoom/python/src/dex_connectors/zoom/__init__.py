# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Dex-native Zoom Meetings connector."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_connection_with_operations, new_project_connection
from .models import (
    CreateMeetingInput,
    GetMeetingInput,
    ListMeetingsInput,
    ListPastMeetingParticipantsInput,
    Meeting,
    MeetingPage,
    MeetingParticipant,
    MeetingSettings,
    MeetingSettingsInput,
    MeetingSummary,
    ParticipantPage,
    UpdatedMeeting,
    UpdateMeetingInput,
)
from .operations import (
    CreateMeetingOperation,
    GetMeetingOperation,
    ListMeetingsOperation,
    ListPastMeetingParticipantsOperation,
    UpdateMeetingOperation,
)
from .steps import (
    new_create_meeting_step,
    new_get_meeting_step,
    new_list_meetings_step,
    new_list_past_meeting_participants_step,
    new_update_meeting_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_connection_with_operations",
    "new_project_connection",
    "CreateMeetingInput",
    "GetMeetingInput",
    "ListMeetingsInput",
    "ListPastMeetingParticipantsInput",
    "Meeting",
    "MeetingPage",
    "MeetingParticipant",
    "MeetingSettings",
    "MeetingSettingsInput",
    "MeetingSummary",
    "ParticipantPage",
    "UpdatedMeeting",
    "UpdateMeetingInput",
    "CreateMeetingOperation",
    "GetMeetingOperation",
    "ListMeetingsOperation",
    "ListPastMeetingParticipantsOperation",
    "UpdateMeetingOperation",
    "new_create_meeting_step",
    "new_get_meeting_step",
    "new_list_meetings_step",
    "new_list_past_meeting_participants_step",
    "new_update_meeting_step",
]
