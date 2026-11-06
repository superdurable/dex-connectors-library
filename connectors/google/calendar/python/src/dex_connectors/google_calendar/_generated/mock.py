# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import BusyInterval, CalendarFreeBusy, CreateEventInput, CreateEventOutput, Event, EventAttendee, EventAttendeeInput, EventConference, EventDateTime, FreeBusyError, GetEventInput, ListEventsInput, ListEventsOutput, QueryFreeBusyInput, QueryFreeBusyOutput, UpdateEventInput, UpdateEventOutput
from .definitions import CONNECTOR_ID, PROVIDER, CONFLICT, CREATED, CREATE_EVENT_DEFINITION, DEFECT, FOUND, GET_EVENT_DEFINITION, INCOMPLETE, INVALID_RESPONSE, LISTED, LIST_EVENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, QUERIED, QUERY_FREE_BUSY_DEFINITION, UPDATED, UPDATE_EVENT_DEFINITION

def list_events_listed(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_events_listed, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_not_found(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_events_not_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_provider_rejected(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_events_provider_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_invalid_response(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_events_invalid_response, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_defect(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_events_defect, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_retry(failure: Failure, after_seconds: float = 0) -> Case[ListEventsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_events_retry, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_meeting_window() -> Case[ListEventsOutput]:
    return Case.paged(LISTED, (ListEventsOutput(calendar_id='primary', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=(Event(id='meetinghold0001', calendar_id='primary', status='confirmed', summary='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque', etag='"mock-etag-1"'), Event(id='focusblock0001', calendar_id='primary', status='confirmed', summary='Focus time', start=EventDateTime(date_time='2026-02-20T14:15:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T14:45:00-08:00', time_zone='America/Los_Angeles'), transparency='transparent')), next_page_token='calendar-page-2'), ListEventsOutput(calendar_id='primary', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=(Event(id='focusblock0001', calendar_id='primary', status='confirmed', summary='Focus time', start=EventDateTime(date_time='2026-02-20T14:15:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T14:45:00-08:00', time_zone='America/Los_Angeles'), transparency='transparent'), Event(id='focusblock0002', calendar_id='primary', status='confirmed', summary='Private focus', start=EventDateTime(date_time='2026-02-20T14:30:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T14:50:00-08:00', time_zone='America/Los_Angeles'), transparency='transparent')), next_page_token='calendar-page-3'), ListEventsOutput(calendar_id='primary', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=())))

setattr(list_events_meeting_window, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_busy_meeting_window() -> Case[ListEventsOutput]:
    return Case.paged(LISTED, (ListEventsOutput(calendar_id='primary', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=(Event(id='meetinghold0001', calendar_id='primary', status='confirmed', summary='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque'), Event(id='boardprep0001', calendar_id='primary', status='confirmed', summary='Board preparation', start=EventDateTime(date_time='2026-02-20T14:30:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:30:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque')), next_page_token='busy-page-2'), ListEventsOutput(calendar_id='primary', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=(Event(id='boardprep0001', calendar_id='primary', status='confirmed', summary='Board preparation', start=EventDateTime(date_time='2026-02-20T14:30:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:30:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque'),))))

setattr(list_events_busy_meeting_window, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_calendar_not_found() -> Case[ListEventsOutput]:
    return Case.branch_result(NOT_FOUND, ListEventsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listEvents", "The requested calendar was not found."))

setattr(list_events_calendar_not_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def list_events_list_rejected() -> Case[ListEventsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListEventsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listEvents", "Google Calendar rejected the event listing."))

setattr(list_events_list_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_found(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_event_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_not_found(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_event_not_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_provider_rejected(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_event_provider_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_invalid_response(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_event_invalid_response, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_defect(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_event_defect, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_retry(failure: Failure, after_seconds: float = 0) -> Case[Event]:
    return Case.retry(failure, after_seconds)

setattr(get_event_retry, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_meeting_hold() -> Case[Event]:
    return Case.branch_result(FOUND, Event(id='meetinghold0001', calendar_id='primary', status='confirmed', summary='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque', etag='"mock-etag-1"'))

setattr(get_event_meeting_hold, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_event_not_found() -> Case[Event]:
    return Case.branch_result(NOT_FOUND, Event(start=EventDateTime(), end=EventDateTime()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getEvent", "The requested event was not found."))

setattr(get_event_event_not_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def get_event_read_rejected() -> Case[Event]:
    return Case.branch_result(PROVIDER_REJECTED, Event(start=EventDateTime(), end=EventDateTime()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getEvent", "Google Calendar rejected the event read."))

setattr(get_event_read_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_created(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_event_created, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_conflict(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(CONFLICT, value, failure)

setattr(create_event_conflict, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_provider_rejected(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_event_provider_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_invalid_response(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_event_invalid_response, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_defect(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_event_defect, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateEventOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_event_retry, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_hold_created() -> Case[CreateEventOutput]:
    return Case.branch_result(CREATED, CreateEventOutput(event=Event(id='meetinghold0001', calendar_id='primary', status='confirmed', summary='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque', etag='"mock-etag-1"'), was_already_created=False))

setattr(create_event_hold_created, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_event_id_conflict() -> Case[CreateEventOutput]:
    return Case.branch_result(CONFLICT, CreateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.CONFLICT, CONNECTOR_ID, "createEvent", "The stable event ID cannot be reused."))

setattr(create_event_event_id_conflict, '__module__', "dex_connectors.google_calendar.calendarmock")

def create_event_create_rejected() -> Case[CreateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createEvent", "Google Calendar rejected the event creation."))

setattr(create_event_create_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_updated(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_event_updated, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_not_found(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_event_not_found, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_provider_rejected(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_event_provider_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_invalid_response(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_event_invalid_response, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_defect(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_event_defect, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateEventOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_event_retry, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_attendees_invited() -> Case[UpdateEventOutput]:
    return Case.branch_result(UPDATED, UpdateEventOutput(event=Event(id='meetinghold0001', calendar_id='primary', status='confirmed', summary='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), transparency='opaque', attendees=(EventAttendee(email='priya@meridian.example.com', response_status='needsAction'), EventAttendee(email='alice@example.com', response_status='needsAction')), etag='"mock-etag-2"')))

setattr(update_event_attendees_invited, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_not_found_mock() -> Case[UpdateEventOutput]:
    return Case.branch_result(NOT_FOUND, UpdateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateEvent", "The event was not found for update."))

setattr(update_event_not_found_mock, '__module__', "dex_connectors.google_calendar.calendarmock")

def update_event_update_rejected() -> Case[UpdateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateEvent", "Google Calendar rejected the event update."))

setattr(update_event_update_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_queried(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(QUERIED, value, failure)

setattr(query_free_busy_queried, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_incomplete(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INCOMPLETE, value, failure)

setattr(query_free_busy_incomplete, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_provider_rejected(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_free_busy_provider_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_invalid_response(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_free_busy_invalid_response, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_defect(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_free_busy_defect, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_retry(failure: Failure, after_seconds: float = 0) -> Case[QueryFreeBusyOutput]:
    return Case.retry(failure, after_seconds)

setattr(query_free_busy_retry, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_calendar_free() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(QUERIED, QueryFreeBusyOutput(time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', calendars=(CalendarFreeBusy(calendar_id='primary', busy=()),)))

setattr(query_free_busy_calendar_free, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_availability_incomplete() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INCOMPLETE, QueryFreeBusyOutput(time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', calendars=(CalendarFreeBusy(calendar_id='primary', busy=(), errors=(FreeBusyError(domain='global', reason='notFound'),)),)), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "queryFreeBusy", "Google Calendar could not report availability for every calendar."))

setattr(query_free_busy_availability_incomplete, '__module__', "dex_connectors.google_calendar.calendarmock")

def query_free_busy_free_busy_rejected() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, QueryFreeBusyOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "queryFreeBusy", "Google Calendar rejected the free/busy query."))

setattr(query_free_busy_free_busy_rejected, '__module__', "dex_connectors.google_calendar.calendarmock")


class Mock:

    def __init__(self, connection_name: str = "google-calendar") -> None:
        self.list_events_script: Query[ListEventsInput, ListEventsOutput] = Query(LIST_EVENTS_DEFINITION)
        self.list_events_script.default(list_events_meeting_window())
        self.get_event_script: Query[GetEventInput, Event] = Query(GET_EVENT_DEFINITION)
        self.get_event_script.default(get_event_meeting_hold())
        self.create_event_script: Mutation[CreateEventInput, CreateEventOutput] = Mutation(CREATE_EVENT_DEFINITION)
        self.create_event_script.default(create_event_hold_created())
        self.update_event_script: Mutation[UpdateEventInput, UpdateEventOutput] = Mutation(UPDATE_EVENT_DEFINITION)
        self.update_event_script.default(update_event_attendees_invited())
        self.query_free_busy_script: Query[QueryFreeBusyInput, QueryFreeBusyOutput] = Query(QUERY_FREE_BUSY_DEFINITION)
        self.query_free_busy_script.default(query_free_busy_calendar_free())
        self._connection = new_connection_with_operations(
            list_events=self.list_events_script,
            get_event=self.get_event_script,
            create_event=self.create_event_script,
            update_event=self.update_event_script,
            query_free_busy=self.query_free_busy_script,
            reference=ConnectionRef(PROVIDER, connection_name),
        )

    def connection(self) -> Connection:
        return self._connection

setattr(Mock, '__module__', "dex_connectors.google_calendar.calendarmock")

__all__ = [
    "Mock",
    "list_events_listed",
    "list_events_not_found",
    "list_events_provider_rejected",
    "list_events_invalid_response",
    "list_events_defect",
    "list_events_retry",
    "list_events_meeting_window",
    "list_events_busy_meeting_window",
    "list_events_calendar_not_found",
    "list_events_list_rejected",
    "get_event_found",
    "get_event_not_found",
    "get_event_provider_rejected",
    "get_event_invalid_response",
    "get_event_defect",
    "get_event_retry",
    "get_event_meeting_hold",
    "get_event_event_not_found",
    "get_event_read_rejected",
    "create_event_created",
    "create_event_conflict",
    "create_event_provider_rejected",
    "create_event_invalid_response",
    "create_event_defect",
    "create_event_retry",
    "create_event_hold_created",
    "create_event_event_id_conflict",
    "create_event_create_rejected",
    "update_event_updated",
    "update_event_not_found",
    "update_event_provider_rejected",
    "update_event_invalid_response",
    "update_event_defect",
    "update_event_retry",
    "update_event_attendees_invited",
    "update_event_not_found_mock",
    "update_event_update_rejected",
    "query_free_busy_queried",
    "query_free_busy_incomplete",
    "query_free_busy_provider_rejected",
    "query_free_busy_invalid_response",
    "query_free_busy_defect",
    "query_free_busy_retry",
    "query_free_busy_calendar_free",
    "query_free_busy_availability_incomplete",
    "query_free_busy_free_busy_rejected",
]
