# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from datetime import datetime

from dex_connectors.sdk import ConnectionRef, Failure, FailureKind
from dex_connectors.sdk.connectormock import Case, Mutation, Query

from ..connection import Connection, new_connection_with_operations
from ..models import CreateEventInput, CreateEventOutput, Event, EventAttendee, EventAttendeeInput, EventDateTime, EventOnlineMeeting, GetEventInput, ListEventsInput, ListEventsOutput, QueryFreeBusyInput, QueryFreeBusyOutput, ScheduleAvailability, ScheduleItem, UpdateEventInput, UpdateEventOutput
from .definitions import CONNECTOR_ID, PROVIDER, CREATED, CREATE_EVENT_DEFINITION, DEFECT, FOUND, GET_EVENT_DEFINITION, INCOMPLETE, INVALID_RESPONSE, LISTED, LIST_EVENTS_DEFINITION, NOT_FOUND, PROVIDER_REJECTED, QUERIED, QUERY_FREE_BUSY_DEFINITION, UPDATED, UPDATE_EVENT_DEFINITION

def list_events_listed(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(LISTED, value, failure)

setattr(list_events_listed, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_not_found(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(list_events_not_found, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_provider_rejected(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(list_events_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_invalid_response(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(list_events_invalid_response, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_defect(value: ListEventsOutput, failure: Failure | None = None) -> Case[ListEventsOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(list_events_defect, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_retry(failure: Failure, after_seconds: float = 0) -> Case[ListEventsOutput]:
    return Case.retry(failure, after_seconds)

setattr(list_events_retry, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_listed_default_answer() -> Case[ListEventsOutput]:
    return Case.branch_result(LISTED, ListEventsOutput(calendar_id='team-calendar', time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', events=(Event(id='hold-1', calendar_id='team-calendar', subject='Meridian kickoff', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), is_organizer=True, show_as='busy', online_meeting=EventOnlineMeeting(provider='teamsForBusiness', join_url='https://teams.microsoft.com/l/meetup-join/hold-1')), Event(id='free-briefing-1', calendar_id='team-calendar', subject='Optional briefing', start=EventDateTime(date_time='2026-02-20T14:15:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T14:45:00-08:00', time_zone='America/Los_Angeles'), show_as='free')), next_page_token=''))

setattr(list_events_listed_default_answer, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_missing_calendar() -> Case[ListEventsOutput]:
    return Case.branch_result(NOT_FOUND, ListEventsOutput(), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "listEvents", "Listing a missing calendar returns not found."))

setattr(list_events_missing_calendar, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def list_events_denied_calendar() -> Case[ListEventsOutput]:
    return Case.branch_result(PROVIDER_REJECTED, ListEventsOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "listEvents", "Listing a calendar without permission is rejected."))

setattr(list_events_denied_calendar, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_found(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(FOUND, value, failure)

setattr(get_event_found, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_not_found(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(get_event_not_found, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_provider_rejected(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(get_event_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_invalid_response(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(get_event_invalid_response, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_defect(value: Event, failure: Failure | None = None) -> Case[Event]:
    return Case.branch_result(DEFECT, value, failure)

setattr(get_event_defect, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_retry(failure: Failure, after_seconds: float = 0) -> Case[Event]:
    return Case.retry(failure, after_seconds)

setattr(get_event_retry, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_found_default_answer() -> Case[Event]:
    return Case.branch_result(FOUND, Event(id='hold-1', subject='Meridian kickoff', body='Agenda to follow.', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), is_organizer=True, show_as='busy', online_meeting=EventOnlineMeeting(provider='teamsForBusiness', join_url='https://teams.microsoft.com/l/meetup-join/hold-1')))

setattr(get_event_found_default_answer, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_missing_event() -> Case[Event]:
    return Case.branch_result(NOT_FOUND, Event(start=EventDateTime(), end=EventDateTime()), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "getEvent", "Reading a missing event ID returns not found."))

setattr(get_event_missing_event, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def get_event_denied_event() -> Case[Event]:
    return Case.branch_result(PROVIDER_REJECTED, Event(start=EventDateTime(), end=EventDateTime()), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "getEvent", "Reading an event without permission is rejected."))

setattr(get_event_denied_event, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_created(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(CREATED, value, failure)

setattr(create_event_created, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_provider_rejected(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(create_event_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_invalid_response(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(create_event_invalid_response, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_defect(value: CreateEventOutput, failure: Failure | None = None) -> Case[CreateEventOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(create_event_defect, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_retry(failure: Failure, after_seconds: float = 0) -> Case[CreateEventOutput]:
    return Case.retry(failure, after_seconds)

setattr(create_event_retry, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_created_default_answer() -> Case[CreateEventOutput]:
    return Case.branch_result(CREATED, CreateEventOutput(event=Event(id='hold-1', calendar_id='team-calendar', subject='Meridian kickoff', body='Agenda to follow.', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), is_organizer=True, show_as='busy', online_meeting=EventOnlineMeeting(provider='teamsForBusiness', join_url='https://teams.microsoft.com/l/meetup-join/hold-1')), was_already_created=False))

setattr(create_event_created_default_answer, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def create_event_denied_create() -> Case[CreateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, CreateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "createEvent", "Creating an event in a read-only calendar is rejected."))

setattr(create_event_denied_create, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_updated(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(UPDATED, value, failure)

setattr(update_event_updated, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_not_found(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(NOT_FOUND, value, failure)

setattr(update_event_not_found, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_provider_rejected(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(update_event_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_invalid_response(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(update_event_invalid_response, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_defect(value: UpdateEventOutput, failure: Failure | None = None) -> Case[UpdateEventOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(update_event_defect, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_retry(failure: Failure, after_seconds: float = 0) -> Case[UpdateEventOutput]:
    return Case.retry(failure, after_seconds)

setattr(update_event_retry, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_updated_default_answer() -> Case[UpdateEventOutput]:
    return Case.branch_result(UPDATED, UpdateEventOutput(event=Event(id='hold-1', subject='Meridian kickoff', body='Agenda to follow.', start=EventDateTime(date_time='2026-02-20T14:00:00-08:00', time_zone='America/Los_Angeles'), end=EventDateTime(date_time='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles'), is_organizer=True, show_as='busy', attendees=(EventAttendee(email='priya@meridian.example.com'), EventAttendee(email='alice@example.com')), online_meeting=EventOnlineMeeting(provider='teamsForBusiness', join_url='https://teams.microsoft.com/l/meetup-join/hold-1')), was_already_applied=False))

setattr(update_event_updated_default_answer, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_missing_event() -> Case[UpdateEventOutput]:
    return Case.branch_result(NOT_FOUND, UpdateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.NOT_FOUND, CONNECTOR_ID, "updateEvent", "Updating a missing event ID returns not found."))

setattr(update_event_missing_event, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def update_event_denied_update() -> Case[UpdateEventOutput]:
    return Case.branch_result(PROVIDER_REJECTED, UpdateEventOutput(event=Event(start=EventDateTime(), end=EventDateTime())), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "updateEvent", "Updating an event without write permission is rejected."))

setattr(update_event_denied_update, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_queried(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(QUERIED, value, failure)

setattr(query_free_busy_queried, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_incomplete(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INCOMPLETE, value, failure)

setattr(query_free_busy_incomplete, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_provider_rejected(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, value, failure)

setattr(query_free_busy_provider_rejected, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_invalid_response(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INVALID_RESPONSE, value, failure)

setattr(query_free_busy_invalid_response, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_defect(value: QueryFreeBusyOutput, failure: Failure | None = None) -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(DEFECT, value, failure)

setattr(query_free_busy_defect, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_retry(failure: Failure, after_seconds: float = 0) -> Case[QueryFreeBusyOutput]:
    return Case.retry(failure, after_seconds)

setattr(query_free_busy_retry, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_queried_default_answer() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(QUERIED, QueryFreeBusyOutput(time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', availability_view_interval=30, schedules=(ScheduleAvailability(schedule_id='priya@meridian.example.com', is_busy=False, is_availability_known=True, items=(), availability_view='00'), ScheduleAvailability(schedule_id='alice@example.com', is_busy=False, is_availability_known=True, items=(), availability_view='00'))))

setattr(query_free_busy_queried_default_answer, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_unknown_schedule() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(INCOMPLETE, QueryFreeBusyOutput(time_min='2026-02-20T14:00:00-08:00', time_max='2026-02-20T15:00:00-08:00', time_zone='America/Los_Angeles', availability_view_interval=30, schedules=(ScheduleAvailability(schedule_id='alice@example.com', is_busy=False, is_availability_known=False, unknown_reason='error', error_response_code='ErrorAccessDenied', items=(), availability_view=''),)), Failure(FailureKind.PROVIDER_REJECTION, CONNECTOR_ID, "queryFreeBusy", "Querying only alice@example.com when Graph cannot read her schedule returns unknown availability."))

setattr(query_free_busy_unknown_schedule, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

def query_free_busy_denied_schedule() -> Case[QueryFreeBusyOutput]:
    return Case.branch_result(PROVIDER_REJECTED, QueryFreeBusyOutput(), Failure(FailureKind.AUTHORIZATION, CONNECTOR_ID, "queryFreeBusy", "Querying schedules without permission is rejected."))

setattr(query_free_busy_denied_schedule, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")


class Mock:

    def __init__(self, connection_name: str = "outlook-calendar") -> None:
        self.list_events_script: Query[ListEventsInput, ListEventsOutput] = Query(LIST_EVENTS_DEFINITION)
        self.list_events_script.default(list_events_listed_default_answer())
        self.get_event_script: Query[GetEventInput, Event] = Query(GET_EVENT_DEFINITION)
        self.get_event_script.default(get_event_found_default_answer())
        self.create_event_script: Mutation[CreateEventInput, CreateEventOutput] = Mutation(CREATE_EVENT_DEFINITION)
        self.create_event_script.default(create_event_created_default_answer())
        self.update_event_script: Mutation[UpdateEventInput, UpdateEventOutput] = Mutation(UPDATE_EVENT_DEFINITION)
        self.update_event_script.default(update_event_updated_default_answer())
        self.query_free_busy_script: Query[QueryFreeBusyInput, QueryFreeBusyOutput] = Query(QUERY_FREE_BUSY_DEFINITION)
        self.query_free_busy_script.default(query_free_busy_queried_default_answer())
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

setattr(Mock, '__module__', "dex_connectors.microsoft_outlook_calendar.outlookcalendarmock")

__all__ = [
    "Mock",
    "list_events_listed",
    "list_events_not_found",
    "list_events_provider_rejected",
    "list_events_invalid_response",
    "list_events_defect",
    "list_events_retry",
    "list_events_listed_default_answer",
    "list_events_missing_calendar",
    "list_events_denied_calendar",
    "get_event_found",
    "get_event_not_found",
    "get_event_provider_rejected",
    "get_event_invalid_response",
    "get_event_defect",
    "get_event_retry",
    "get_event_found_default_answer",
    "get_event_missing_event",
    "get_event_denied_event",
    "create_event_created",
    "create_event_provider_rejected",
    "create_event_invalid_response",
    "create_event_defect",
    "create_event_retry",
    "create_event_created_default_answer",
    "create_event_denied_create",
    "update_event_updated",
    "update_event_not_found",
    "update_event_provider_rejected",
    "update_event_invalid_response",
    "update_event_defect",
    "update_event_retry",
    "update_event_updated_default_answer",
    "update_event_missing_event",
    "update_event_denied_update",
    "query_free_busy_queried",
    "query_free_busy_incomplete",
    "query_free_busy_provider_rejected",
    "query_free_busy_invalid_response",
    "query_free_busy_defect",
    "query_free_busy_retry",
    "query_free_busy_queried_default_answer",
    "query_free_busy_unknown_schedule",
    "query_free_busy_denied_schedule",
]
