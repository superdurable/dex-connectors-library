# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
# ruff: noqa

from dataclasses import dataclass, field as _dataclass_field
from typing import Any, Optional

def _replace_nil_slices(value: Any, depth: int) -> Any:
    if value is None:
        return ()
    if depth == 1:
        return tuple(value)
    return tuple(_replace_nil_slices(item, depth - 1) for item in value)


# ListEventsInput selects one page of events on one calendar that overlap a bounded window.
# Google returns an event when its end is after TimeMin and its start is before TimeMax.
# Recurring events are expanded into their instances and the page is ordered by start.
@dataclass(frozen=True, slots=True)
class ListEventsInput:
    # CalendarID is a calendar ID from the calendar picker, or primary for the authorized user's calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # TimeMin is the required window start as RFC 3339 with an explicit offset.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # Query is optional free text Google matches against summary, description, location, and guests.
    query: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # PageSize is the maximum events in the page, from 1 to 250; zero requests 50.
    # Google may return fewer events, even none, before the last page.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page. Keep every other field unchanged when paging.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})
    # TimeZone is an optional IANA zone for the offsets of returned dateTime values; blank uses the calendar's zone.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(ListEventsInput, '__module__', 'dex_connectors.google_calendar.models')


# EventDateTime is one event boundary in Google Calendar's native shape. Exactly one
# of Date and DateTime is set, so a boundary is unambiguously all-day or timed.
#
# A timed boundary sets DateTime to an RFC 3339 instant with an explicit Z or
# ±hh:mm offset, such as 2026-02-24T09:00:00-08:00, and TimeZone to an IANA name
# such as America/Los_Angeles. The offset fixes the instant; TimeZone is the zone
# Google uses to display the event. A value without an offset, such as
# 2026-02-24T09:00:00, is rejected rather than guessed.
#
# An all-day boundary sets Date to YYYY-MM-DD and leaves TimeZone blank: an
# all-day date has no instant until it is read in a calendar's time zone. An
# all-day end date is exclusive, so a one-day event on 24 February ends on
# 2026-02-25.
@dataclass(frozen=True, slots=True)
class EventDateTime:
    # Date is the YYYY-MM-DD calendar date of an all-day boundary.
    date: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # DateTime is the RFC 3339 instant, with an explicit offset, of a timed boundary.
    date_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'dateTime', 'dex_json_omit_default': True})
    # TimeZone is the IANA time zone of a timed boundary. Google may also report one on output.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(EventDateTime, '__module__', 'dex_connectors.google_calendar.models')


# EventAttendee is one guest of an event.
@dataclass(frozen=True, slots=True)
class EventAttendee:
    # Email is the guest's email address.
    email: str = ''
    # DisplayName is the guest's name when Google knows it.
    display_name: str = _dataclass_field(default='', metadata={'dex_json_name': 'displayName', 'dex_json_omit_default': True})
    # ResponseStatus is Google's value: needsAction, declined, tentative, or accepted.
    response_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseStatus', 'dex_json_omit_default': True})
    # IsOptional reports an optional guest.
    is_optional: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOptional'})
    # IsOrganizer reports the event organizer.
    is_organizer: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOrganizer'})
    # IsSelf reports the guest whose calendar was read.
    is_self: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isSelf'})
    # IsResource reports a room or other resource.
    is_resource: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isResource'})


setattr(EventAttendee, '__module__', 'dex_connectors.google_calendar.models')


# EventConference describes the conference attached to an event.
@dataclass(frozen=True, slots=True)
class EventConference:
    # SolutionType is Google's conference solution key, such as hangoutsMeet.
    solution_type: str = _dataclass_field(default='', metadata={'dex_json_name': 'solutionType', 'dex_json_omit_default': True})
    # ConferenceID is the provider conference ID.
    conference_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'conferenceId', 'dex_json_omit_default': True})
    # JoinURL is the video entry point URL.
    join_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'joinUrl', 'dex_json_omit_default': True})
    # CreateRequestStatus is pending, success, or failure after createEvent requests a conference.
    # Google creates the conference asynchronously, so pending may precede a JoinURL.
    create_request_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'createRequestStatus', 'dex_json_omit_default': True})


setattr(EventConference, '__module__', 'dex_connectors.google_calendar.models')


# Event is one Google Calendar event as returned by listEvents, getEvent, createEvent, and updateEvent.
# Provider enums such as Status, Transparency, and attendee ResponseStatus keep Google's own values.
@dataclass(frozen=True, slots=True)
class Event:
    # ID is the Google Calendar event ID; a recurring instance ID also encodes its original start.
    id: str = ''
    # CalendarID is the calendar the event was read from or written to.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # Status is Google's status: confirmed, tentative, or cancelled.
    status: str = ''
    # Summary is the event title.
    summary: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Description is the event description, which may contain HTML.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Location is the free-text event location.
    location: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Start is the inclusive start; see EventDateTime. It is empty only for some cancelled events.
    start: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # End is the exclusive end; see EventDateTime. It is empty only for some cancelled events.
    end: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # IsAllDay reports that Start and End are dates rather than instants.
    is_all_day: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAllDay'})
    # Transparency is opaque when the event blocks time or transparent when it does not.
    # Google omits the field for opaque events; the connector reports opaque explicitly.
    transparency: str = ''
    # Visibility is Google's visibility value, such as default, public, or private.
    visibility: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Attendees lists the event guests in Google's order.
    attendees: tuple[EventAttendee, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # OrganizerEmail is the organizer's calendar address.
    organizer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizerEmail', 'dex_json_omit_default': True})
    # RecurringEventID is the parent recurring event ID of an expanded instance.
    recurring_event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'recurringEventId', 'dex_json_omit_default': True})
    # OriginalStartTime is the scheduled start of a recurring instance before any change.
    original_start_time: EventDateTime | None = _dataclass_field(default=None, metadata={'dex_json_name': 'originalStartTime', 'dex_json_omit_default': True})
    # Conference describes attached conferencing, such as a Google Meet link.
    conference: EventConference | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # HTMLLink opens the event in the Google Calendar web UI.
    html_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'htmlLink', 'dex_json_omit_default': True})
    # ICalUID is the RFC 5545 identifier shared by every instance of a recurring event.
    i_cal_uid: str = _dataclass_field(default='', metadata={'dex_json_name': 'iCalUID', 'dex_json_omit_default': True})
    # ETag is the event version used for conditional updates.
    etag: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Created is the RFC 3339 creation time reported by Google.
    created: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Updated is the RFC 3339 last-modification time reported by Google.
    updated: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})


setattr(Event, '__module__', 'dex_connectors.google_calendar.models')


# ListEventsOutput is one page of events and the window Google checked.
@dataclass(frozen=True, slots=True)
class ListEventsOutput:
    # CalendarID is the listed calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # TimeMin echoes the checked window start.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax echoes the checked window end.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # TimeZone is the calendar's IANA zone, which gives all-day dates their instants.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # Events lists the page's events ordered by start. Cancelled events are omitted.
    events: tuple[Event, ...] = ()
    # NextPageToken requests the next page, or is empty on the last page.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


ListEventsOutput.__annotations__['events'] = tuple[Event, ...] | None
ListEventsOutput.__dataclass_fields__['events'].type = tuple[Event, ...] | None
setattr(ListEventsOutput, '__module__', 'dex_connectors.google_calendar.models')


# GetEventInput identifies one event to read.
@dataclass(frozen=True, slots=True)
class GetEventInput:
    # CalendarID is the calendar that holds the event, or primary for the authorized user's calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # EventID is the Google event ID, including an expanded recurring instance ID.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    # TimeZone is an optional IANA zone for the offsets of returned dateTime values; blank uses the calendar's zone.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(GetEventInput, '__module__', 'dex_connectors.google_calendar.models')


# EventAttendeeInput is one guest supplied to createEvent or updateEvent.
@dataclass(frozen=True, slots=True)
class EventAttendeeInput:
    # Email is one bare address such as person@example.com, without a display name.
    email: str = ''
    # IsOptional marks the guest as optional.
    is_optional: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOptional', 'dex_json_omit_default': True})


setattr(EventAttendeeInput, '__module__', 'dex_connectors.google_calendar.models')


# CreateEventInput describes one event to create. The event is written under a stable
# client-supplied ID: EventID when set, otherwise one derived from the Step's Call ID.
# A retried Step therefore finds and returns its own event instead of creating another.
@dataclass(frozen=True, slots=True)
class CreateEventInput:
    # CalendarID is a writable calendar ID from the calendar picker, or primary for the authorized user's calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # EventID optionally fixes the event ID for business-level deduplication across Step executions.
    # It must be 5 to 1024 lowercase base32hex characters (a-v and 0-9); blank derives the ID from the Call ID.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId', 'dex_json_omit_default': True})
    # Summary is the required event title.
    summary: str = ''
    # Description is the optional event description.
    description: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Start is the inclusive start; see EventDateTime and ValidateEventTimes.
    start: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # End is the exclusive end; see EventDateTime and ValidateEventTimes.
    end: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # Attendees lists unique bare guest addresses; empty creates an event without guests.
    attendees: tuple[EventAttendeeInput, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # RequestConference asks Google to attach a new Google Meet conference to the event.
    request_conference: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'requestConference', 'dex_json_omit_default': True})
    # SendUpdates is none, all, or externalOnly; blank sends no guest notifications.
    send_updates: str = _dataclass_field(default='', metadata={'dex_json_name': 'sendUpdates', 'dex_json_omit_default': True})


setattr(CreateEventInput, '__module__', 'dex_connectors.google_calendar.models')


# CreateEventOutput is the event that exists under the stable event ID.
@dataclass(frozen=True, slots=True)
class CreateEventOutput:
    # Event is the created or previously created event.
    event: Event = _dataclass_field(default_factory=Event)
    # WasAlreadyCreated reports that the ID already held this event, such as after a retried attempt.
    was_already_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyCreated'})


setattr(CreateEventOutput, '__module__', 'dex_connectors.google_calendar.models')


# UpdateEventInput patches named fields of one existing event. A nil field is left unchanged.
# Start and End are set together, and follow the same rules as createEvent, so an event can
# move between timed and all-day without keeping a stale boundary.
@dataclass(frozen=True, slots=True)
class UpdateEventInput:
    # CalendarID is the calendar that holds the event, or primary for the authorized user's calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # EventID is the Google event ID, including an expanded recurring instance ID.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    # Summary replaces the title when set; it cannot be blank.
    summary: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Description replaces the description when set; an empty string clears it.
    description: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Start replaces the inclusive start when set, together with End.
    start: EventDateTime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # End replaces the exclusive end when set, together with Start.
    end: EventDateTime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Attendees replaces the complete guest list when set; an empty list removes every guest.
    attendees: tuple[EventAttendeeInput, ...] | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # SendUpdates is none, all, or externalOnly; blank sends no guest notifications.
    send_updates: str = _dataclass_field(default='', metadata={'dex_json_name': 'sendUpdates', 'dex_json_omit_default': True})


setattr(UpdateEventInput, '__module__', 'dex_connectors.google_calendar.models')


# UpdateEventOutput is the event after the patch.
@dataclass(frozen=True, slots=True)
class UpdateEventOutput:
    # Event is the event that holds the requested values.
    event: Event = _dataclass_field(default_factory=Event)
    # WasAlreadyApplied reports that the event already held every requested value, so no patch was sent.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied'})


setattr(UpdateEventOutput, '__module__', 'dex_connectors.google_calendar.models')


# QueryFreeBusyInput asks for the busy intervals of calendars in a bounded window.
# Google counts only events that block time: transparent and cancelled events are free.
@dataclass(frozen=True, slots=True)
class QueryFreeBusyInput:
    # CalendarIDs lists 1 to 50 unique calendar IDs, such as primary or colleague@example.com.
    # Group IDs are not supported.
    calendar_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'calendarIds'})
    # TimeMin is the required window start as RFC 3339 with an explicit offset.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # TimeZone is an optional IANA zone for the offsets of returned intervals; blank uses UTC.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'calendar_ids', _replace_nil_slices(self.calendar_ids, 1))


QueryFreeBusyInput.__annotations__['calendar_ids'] = tuple[str, ...] | None
QueryFreeBusyInput.__dataclass_fields__['calendar_ids'].type = tuple[str, ...] | None
setattr(QueryFreeBusyInput, '__module__', 'dex_connectors.google_calendar.models')


# BusyInterval is one busy time range.
@dataclass(frozen=True, slots=True)
class BusyInterval:
    # Start is the inclusive start as RFC 3339 with an explicit offset.
    start: str = ''
    # End is the exclusive end as RFC 3339 with an explicit offset.
    end: str = ''


setattr(BusyInterval, '__module__', 'dex_connectors.google_calendar.models')


# FreeBusyError is one error Google reported for a calendar.
@dataclass(frozen=True, slots=True)
class FreeBusyError:
    # Domain is Google's broad error category, such as global.
    domain: str = ''
    # Reason is Google's reason, such as notFound or internalError; Google may add reasons.
    reason: str = ''


setattr(FreeBusyError, '__module__', 'dex_connectors.google_calendar.models')


# CalendarFreeBusy is the availability of one calendar.
@dataclass(frozen=True, slots=True)
class CalendarFreeBusy:
    # CalendarID is the requested calendar ID.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId'})
    # Busy lists busy intervals ordered as Google returned them; empty means free only when Errors is empty.
    busy: tuple[BusyInterval, ...] = ()
    # Errors lists Google's errors for this calendar. Any error selects the incomplete branch.
    errors: tuple[FreeBusyError, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'busy', _replace_nil_slices(self.busy, 1))


CalendarFreeBusy.__annotations__['busy'] = tuple[BusyInterval, ...] | None
CalendarFreeBusy.__dataclass_fields__['busy'].type = tuple[BusyInterval, ...] | None
setattr(CalendarFreeBusy, '__module__', 'dex_connectors.google_calendar.models')


# QueryFreeBusyOutput holds the busy intervals of every requested calendar in request order.
@dataclass(frozen=True, slots=True)
class QueryFreeBusyOutput:
    # TimeMin is the checked window start.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax is the checked window end.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # TimeZone is the zone of the returned offsets.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # Calendars lists one entry per requested calendar ID.
    calendars: tuple[CalendarFreeBusy, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'calendars', _replace_nil_slices(self.calendars, 1))


QueryFreeBusyOutput.__annotations__['calendars'] = tuple[CalendarFreeBusy, ...] | None
QueryFreeBusyOutput.__dataclass_fields__['calendars'].type = tuple[CalendarFreeBusy, ...] | None
setattr(QueryFreeBusyOutput, '__module__', 'dex_connectors.google_calendar.models')
