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


# ListEventsInput selects one page of a calendar view: the occurrences, exceptions, and
# single instances of events that overlap a bounded window, with recurring series expanded.
@dataclass(frozen=True, slots=True)
class ListEventsInput:
    # CalendarID is a calendar ID from the calendar picker; blank lists the mailbox's default calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId', 'dex_json_omit_default': True})
    # TimeMin is the required window start as RFC 3339 with an explicit offset.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax is the required window end as RFC 3339 with an explicit offset. The window is at most 366 days.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # PageSize is the maximum events in the page, from 1 to 250; zero requests 50.
    # Graph may return fewer events, even none, before the last page.
    page_size: int = _dataclass_field(default=0, metadata={'dex_json_name': 'pageSize', 'dex_json_omit_default': True})
    # PageToken is the NextPageToken of the previous page. Keep every other field unchanged when paging.
    page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'pageToken', 'dex_json_omit_default': True})
    # TimeZone is an optional IANA zone for the offsets of returned times; blank uses UTC.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(ListEventsInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# EventDateTime is one event boundary: an absolute instant plus the IANA time zone
# it is shown in.
#
# DateTime is RFC 3339 with an explicit Z or ±hh:mm offset, such as
# 2026-02-24T09:00:00-08:00, and TimeZone is an IANA name such as
# America/Los_Angeles. The offset fixes the instant; TimeZone is the zone the event
# is written in, so Outlook keeps the organizer's zone. A value without an offset,
# such as 2026-02-24T09:00:00, or without TimeZone, is rejected rather than guessed.
#
# Microsoft Graph itself stores a wall time without an offset plus a zone name. The
# connector writes the wall time of the instant in TimeZone, or the UTC wall time when a
# daylight-saving change makes that wall time ambiguous, so the instant never moves.
# It reads every time back in UTC and returns it with an offset in the requested zone.
@dataclass(frozen=True, slots=True)
class EventDateTime:
    # DateTime is the RFC 3339 instant with an explicit offset.
    date_time: str = _dataclass_field(default='', metadata={'dex_json_name': 'dateTime'})
    # TimeZone is the IANA time zone of the boundary, such as America/Los_Angeles.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})


setattr(EventDateTime, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# EventAttendee is one attendee of an event.
@dataclass(frozen=True, slots=True)
class EventAttendee:
    # Email is the attendee's SMTP address.
    email: str = ''
    # Name is the attendee's display name when Graph knows it.
    name: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is required, optional, or resource.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # ResponseStatus is Graph's response value, such as none, accepted, declined, or tentativelyAccepted.
    response_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseStatus', 'dex_json_omit_default': True})


setattr(EventAttendee, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# EventOnlineMeeting describes the online meeting attached to an event.
@dataclass(frozen=True, slots=True)
class EventOnlineMeeting:
    # Provider is Graph's onlineMeetingProvider, such as teamsForBusiness.
    provider: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # JoinURL is the URL attendees open to join; Graph can add it shortly after creation.
    join_url: str = _dataclass_field(default='', metadata={'dex_json_name': 'joinUrl', 'dex_json_omit_default': True})


setattr(EventOnlineMeeting, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# Event is one Outlook calendar event as returned by listEvents, getEvent, createEvent, and updateEvent.
# Provider enums such as ShowAs, Type, Sensitivity, and attendee ResponseStatus keep Graph's own values.
@dataclass(frozen=True, slots=True)
class Event:
    # ID is the Graph event ID, unique within the mailbox; an expanded occurrence has its own ID.
    id: str = ''
    # CalendarID is the calendar the event was listed from or created in; blank when the operation did not name one.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId', 'dex_json_omit_default': True})
    # Subject is the event title.
    subject: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Body is the plain-text event body; listEvents omits it and returns BodyPreview instead.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # BodyPreview is Graph's first 255 characters of the body as text.
    body_preview: str = _dataclass_field(default='', metadata={'dex_json_name': 'bodyPreview', 'dex_json_omit_default': True})
    # Location is the location display name.
    location: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Start is the inclusive start in the requested display zone; see EventDateTime.
    start: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # End is the exclusive end in the requested display zone; see EventDateTime.
    end: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # OriginalStartTimeZone is the zone the event was created in, as Graph reports it, which may be a Windows zone name.
    original_start_time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'originalStartTimeZone', 'dex_json_omit_default': True})
    # OriginalEndTimeZone is the end's creation zone, as Graph reports it.
    original_end_time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'originalEndTimeZone', 'dex_json_omit_default': True})
    # IsAllDay reports an all-day event; Start and End are then midnights in the event's own zone.
    is_all_day: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAllDay'})
    # IsCancelled reports a meeting the organizer cancelled that still shows on this calendar.
    is_cancelled: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isCancelled'})
    # IsOrganizer reports that the calendar's owner organizes the event.
    is_organizer: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOrganizer'})
    # ShowAs is free, tentative, busy, oof, workingElsewhere, or unknown; unknown includes an omitted value.
    show_as: str = _dataclass_field(default='', metadata={'dex_json_name': 'showAs'})
    # Sensitivity is normal, personal, private, or confidential.
    sensitivity: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Type is singleInstance, occurrence, exception, or seriesMaster.
    type: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # SeriesMasterID is the recurring series an occurrence or exception belongs to.
    series_master_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'seriesMasterId', 'dex_json_omit_default': True})
    # ResponseStatus is the calendar owner's response, such as organizer, accepted, or notResponded.
    response_status: str = _dataclass_field(default='', metadata={'dex_json_name': 'responseStatus', 'dex_json_omit_default': True})
    # Attendees lists the event attendees in Graph's order.
    attendees: tuple[EventAttendee, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # OrganizerEmail is the organizer's SMTP address.
    organizer_email: str = _dataclass_field(default='', metadata={'dex_json_name': 'organizerEmail', 'dex_json_omit_default': True})
    # OnlineMeeting describes an attached online meeting, such as a Teams meeting.
    online_meeting: EventOnlineMeeting | None = _dataclass_field(default=None, metadata={'dex_json_name': 'onlineMeeting', 'dex_json_omit_default': True})
    # WebLink opens the event in Outlook on the web.
    web_link: str = _dataclass_field(default='', metadata={'dex_json_name': 'webLink', 'dex_json_omit_default': True})
    # ICalUID is the RFC 5545 identifier shared by every instance of a recurring event.
    i_cal_u_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'iCalUId', 'dex_json_omit_default': True})
    # ChangeKey changes every time the event changes.
    change_key: str = _dataclass_field(default='', metadata={'dex_json_name': 'changeKey', 'dex_json_omit_default': True})
    # ETag is Graph's @odata.etag, which updateEvent sends as If-Match.
    etag: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # TransactionID is the client transactionId set at creation; Graph returns it only when an app set it.
    transaction_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transactionId', 'dex_json_omit_default': True})


setattr(Event, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# ListEventsOutput is one page of the calendar view.
@dataclass(frozen=True, slots=True)
class ListEventsOutput:
    # CalendarID echoes the listed calendar; blank means the default calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId', 'dex_json_omit_default': True})
    # TimeMin echoes the checked window start.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax echoes the checked window end.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # TimeZone is the IANA zone of every returned offset.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # Events lists the page's events sorted by start instant within the page. Cancelled meetings
    # that still show on the calendar are included with IsCancelled set.
    events: tuple[Event, ...] = ()
    # NextPageToken requests the next page, or is empty on the last page. It is Graph's opaque
    # @odata.nextLink URL; pass it back unchanged.
    next_page_token: str = _dataclass_field(default='', metadata={'dex_json_name': 'nextPageToken', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'events', _replace_nil_slices(self.events, 1))


ListEventsOutput.__annotations__['events'] = tuple[Event, ...] | None
ListEventsOutput.__dataclass_fields__['events'].type = tuple[Event, ...] | None
setattr(ListEventsOutput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# GetEventInput identifies one event to read. Graph event IDs are unique within a
# mailbox, so no calendar ID is needed.
@dataclass(frozen=True, slots=True)
class GetEventInput:
    # EventID is the Graph event ID, including an expanded occurrence ID.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    # TimeZone is an optional IANA zone for the offsets of returned times; blank uses UTC.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(GetEventInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# EventAttendeeInput is one attendee supplied to createEvent or updateEvent.
@dataclass(frozen=True, slots=True)
class EventAttendeeInput:
    # Email is one bare SMTP address such as person@example.com, without a display name.
    email: str = ''
    # IsOptional makes the attendee optional instead of required.
    is_optional: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isOptional', 'dex_json_omit_default': True})


setattr(EventAttendeeInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# CreateEventInput describes one event to create. Every attempt of one Step execution
# uses the same idempotency key: TransactionID when set, otherwise the Step's Call ID.
# The key is sent as Graph's transactionId and stamped on the event as a connector-owned
# extended property, so a retried Step finds and returns its own event.
@dataclass(frozen=True, slots=True)
class CreateEventInput:
    # CalendarID is a writable calendar ID from the calendar picker; blank uses the mailbox's default calendar.
    calendar_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'calendarId', 'dex_json_omit_default': True})
    # TransactionID optionally fixes the idempotency key for business-level deduplication across Step
    # executions: 1 to 64 letters, digits, dots, hyphens, or underscores. Blank uses the Call ID.
    transaction_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'transactionId', 'dex_json_omit_default': True})
    # Subject is the required event title.
    subject: str = ''
    # Body is the optional plain-text event body.
    body: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Location is the optional location display name.
    location: str = _dataclass_field(default='', metadata={'dex_json_omit_default': True})
    # Start is the inclusive start; see EventDateTime and ValidateEventTimes.
    start: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # End is the exclusive end; see EventDateTime and ValidateEventTimes.
    end: EventDateTime = _dataclass_field(default_factory=EventDateTime)
    # IsAllDay creates an all-day event; Start and End must then be midnights in one zone.
    is_all_day: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAllDay', 'dex_json_omit_default': True})
    # Attendees lists unique bare attendee addresses. Graph sends every attendee an invitation
    # when the event is created, and that cannot be turned off; empty creates an event without attendees.
    attendees: tuple[EventAttendeeInput, ...] = _dataclass_field(default=(), metadata={'dex_json_omit_default': True})
    # RequestOnlineMeeting asks Graph to attach a Microsoft Teams meeting to the event.
    request_online_meeting: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'requestOnlineMeeting', 'dex_json_omit_default': True})


setattr(CreateEventInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# CreateEventOutput is the event that carries the Step's idempotency key.
@dataclass(frozen=True, slots=True)
class CreateEventOutput:
    # Event is the created or previously created event.
    event: Event = _dataclass_field(default_factory=Event)
    # WasAlreadyCreated reports that an earlier attempt had already created this event, so no POST was sent.
    was_already_created: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyCreated'})
    # DuplicateEventIDs lists further events with the same idempotency key. Graph's transactionId is meant
    # to prevent them, so a non-empty list means two concurrent attempts both created an event.
    duplicate_event_ids: tuple[str, ...] = _dataclass_field(default=(), metadata={'dex_json_name': 'duplicateEventIds', 'dex_json_omit_default': True})


setattr(CreateEventOutput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# UpdateEventInput patches named fields of one existing event. A nil field is left unchanged.
# Start and End are set together and follow the same rules as createEvent.
@dataclass(frozen=True, slots=True)
class UpdateEventInput:
    # EventID is the Graph event ID, including an occurrence ID.
    event_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'eventId'})
    # Subject replaces the title when set; it cannot be blank.
    subject: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Body replaces the plain-text body when set; an empty string clears it.
    body: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Location replaces the location display name when set; an empty string clears it.
    location: str | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # Start replaces the inclusive start when set, together with End.
    start: EventDateTime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # End replaces the exclusive end when set, together with Start.
    end: EventDateTime | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # IsAllDay applies only with Start and End: true makes the event all-day, and false, the
    # default, makes it timed. It is sent whenever Start and End are, so no stale form remains.
    is_all_day: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAllDay', 'dex_json_omit_default': True})
    # Attendees replaces the complete attendee list when set; an empty list removes every attendee.
    # Graph sends a meeting update only to the attendees that changed.
    attendees: tuple[EventAttendeeInput, ...] | None = _dataclass_field(default=None, metadata={'dex_json_omit_default': True})
    # TimeZone is an optional IANA zone for the offsets of returned times; blank uses Start's zone or UTC.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})


setattr(UpdateEventInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# UpdateEventOutput is the event after the update.
@dataclass(frozen=True, slots=True)
class UpdateEventOutput:
    # Event is the event that holds the requested values.
    event: Event = _dataclass_field(default_factory=Event)
    # WasAlreadyApplied reports that the event already held every requested value, so no PATCH was sent.
    was_already_applied: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'wasAlreadyApplied'})


setattr(UpdateEventOutput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# QueryFreeBusyInput asks Microsoft Graph getSchedule for the availability of users,
# distribution lists, or resources in a bounded window.
@dataclass(frozen=True, slots=True)
class QueryFreeBusyInput:
    # Schedules lists 1 to 20 unique bare SMTP addresses, such as colleague@contoso.com or a room mailbox.
    schedules: tuple[str, ...] = ()
    # TimeMin is the required window start as RFC 3339 with an explicit offset.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax is the required window end as RFC 3339 with an explicit offset. The window must be shorter than 62 days.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # AvailabilityViewInterval is the length in minutes of each AvailabilityView slot, from 5 to 1440; zero requests 30.
    availability_view_interval: int = _dataclass_field(default=0, metadata={'dex_json_name': 'availabilityViewInterval', 'dex_json_omit_default': True})
    # TimeZone is an optional IANA zone for the offsets of returned intervals; blank uses UTC.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone', 'dex_json_omit_default': True})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'schedules', _replace_nil_slices(self.schedules, 1))


QueryFreeBusyInput.__annotations__['schedules'] = tuple[str, ...] | None
QueryFreeBusyInput.__dataclass_fields__['schedules'].type = tuple[str, ...] | None
setattr(QueryFreeBusyInput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# ScheduleItem is one block of time on a schedule.
@dataclass(frozen=True, slots=True)
class ScheduleItem:
    # Start is the inclusive start as RFC 3339 with an explicit offset.
    start: str = ''
    # End is the exclusive end as RFC 3339 with an explicit offset.
    end: str = ''
    # Status is free, tentative, busy, oof, workingElsewhere, or unknown.
    status: str = ''


setattr(ScheduleItem, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# ScheduleAvailability is the availability of one schedule.
@dataclass(frozen=True, slots=True)
class ScheduleAvailability:
    # ScheduleID is the requested SMTP address.
    schedule_id: str = _dataclass_field(default='', metadata={'dex_json_name': 'scheduleId'})
    # IsBusy reports busy, tentative, or out-of-office time in the window, from an item or an AvailabilityView slot.
    is_busy: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isBusy'})
    # IsAvailabilityKnown is false when Graph reported an error, an unknown item, or an unreadable view,
    # or omitted the schedule. Unknown availability is never free, and it selects the incomplete branch.
    is_availability_known: bool = _dataclass_field(default=False, metadata={'dex_json_name': 'isAvailabilityKnown'})
    # UnknownReason is error, missing, unknownStatus, or unreadableAvailabilityView when availability is unknown.
    unknown_reason: str = _dataclass_field(default='', metadata={'dex_json_name': 'unknownReason', 'dex_json_omit_default': True})
    # ErrorResponseCode is Graph's freeBusyError responseCode when it is word-shaped; the message is never kept.
    error_response_code: str = _dataclass_field(default='', metadata={'dex_json_name': 'errorResponseCode', 'dex_json_omit_default': True})
    # Items lists Graph's schedule items, free ones included, in Graph's order.
    items: tuple[ScheduleItem, ...] = ()
    # AvailabilityView is Graph's slot string: 0 free or working elsewhere, 1 tentative, 2 busy, 3 out of office.
    availability_view: str = _dataclass_field(default='', metadata={'dex_json_name': 'availabilityView'})

    def __post_init__(self) -> None:
        object.__setattr__(self, 'items', _replace_nil_slices(self.items, 1))


ScheduleAvailability.__annotations__['items'] = tuple[ScheduleItem, ...] | None
ScheduleAvailability.__dataclass_fields__['items'].type = tuple[ScheduleItem, ...] | None
setattr(ScheduleAvailability, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')


# QueryFreeBusyOutput holds the availability of every requested schedule in request order.
@dataclass(frozen=True, slots=True)
class QueryFreeBusyOutput:
    # TimeMin echoes the checked window start.
    time_min: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMin'})
    # TimeMax echoes the checked window end.
    time_max: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeMax'})
    # TimeZone is the IANA zone of every returned offset.
    time_zone: str = _dataclass_field(default='', metadata={'dex_json_name': 'timeZone'})
    # AvailabilityViewInterval is the slot length in minutes of every AvailabilityView.
    availability_view_interval: int = _dataclass_field(default=0, metadata={'dex_json_name': 'availabilityViewInterval'})
    # Schedules lists one entry per requested address.
    schedules: tuple[ScheduleAvailability, ...] = ()

    def __post_init__(self) -> None:
        object.__setattr__(self, 'schedules', _replace_nil_slices(self.schedules, 1))


QueryFreeBusyOutput.__annotations__['schedules'] = tuple[ScheduleAvailability, ...] | None
QueryFreeBusyOutput.__dataclass_fields__['schedules'].type = tuple[ScheduleAvailability, ...] | None
setattr(QueryFreeBusyOutput, '__module__', 'dex_connectors.microsoft_outlook_calendar.models')
