# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Microsoft Outlook Calendar connector for Dex."""

from .client import Client, Config, Credentials
from .connection import Connection, new_connection, new_project_connection
from .models import (
    CreateEventInput,
    CreateEventOutput,
    Event,
    EventAttendee,
    EventAttendeeInput,
    EventDateTime,
    EventOnlineMeeting,
    GetEventInput,
    ListEventsInput,
    ListEventsOutput,
    QueryFreeBusyInput,
    QueryFreeBusyOutput,
    ScheduleAvailability,
    ScheduleItem,
    UpdateEventInput,
    UpdateEventOutput,
)
from .steps import (
    new_create_event_step,
    new_get_event_step,
    new_list_events_step,
    new_query_free_busy_step,
    new_update_event_step,
)

__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Connection",
    "new_connection",
    "new_project_connection",
    "CreateEventInput",
    "CreateEventOutput",
    "Event",
    "EventAttendee",
    "EventAttendeeInput",
    "EventDateTime",
    "EventOnlineMeeting",
    "GetEventInput",
    "ListEventsInput",
    "ListEventsOutput",
    "QueryFreeBusyInput",
    "QueryFreeBusyOutput",
    "ScheduleAvailability",
    "ScheduleItem",
    "UpdateEventInput",
    "UpdateEventOutput",
    "new_create_event_step",
    "new_get_event_step",
    "new_list_events_step",
    "new_query_free_busy_step",
    "new_update_event_step",
]
