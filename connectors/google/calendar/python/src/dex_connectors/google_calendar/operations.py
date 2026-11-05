# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Google Calendar operation invocations and Go-compatible result mapping."""

import json
import re
from dataclasses import replace
from datetime import UTC, date, datetime, timedelta
from email.utils import parseaddr
from typing import Any
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from dex_connectors.sdk import Call, Failure, FailureKind, MutationAttempt, QueryAttempt, Receipt

from ._generated.definitions import (
    CREATE_EVENT_DEFINITION,
    GET_EVENT_DEFINITION,
    LIST_EVENTS_DEFINITION,
    QUERY_FREE_BUSY_DEFINITION,
    UPDATE_EVENT_DEFINITION,
)
from .client import (
    Client,
    ProviderRequestError,
    ProviderResponse,
    calendar_path,
    classify_status,
    event_path,
    failure,
)
from .models import (
    BusyInterval,
    CalendarFreeBusy,
    CreateEventInput,
    CreateEventOutput,
    Event,
    EventAttendee,
    EventAttendeeInput,
    EventConference,
    EventDateTime,
    FreeBusyError,
    GetEventInput,
    ListEventsInput,
    ListEventsOutput,
    QueryFreeBusyInput,
    QueryFreeBusyOutput,
    UpdateEventInput,
    UpdateEventOutput,
)

_INSTANT = re.compile(r"\A(\d{4}-\d\d-\d\d)T(\d\d):(\d\d):(\d\d)(?:[.,](\d+))?(Z|[+-]\d\d:\d\d)\Z")
_EVENT_ID = re.compile(r"[a-v0-9]{5,1024}\Z")
_INSTANT_ERROR = (
    "must be RFC 3339 with an explicit offset, such as 2026-02-24T09:00:00-08:00 or 2026-02-24T17:00:00Z"
)


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("provider field has an invalid type")
    return value


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("provider object has an invalid type")
    return value


def _array(value: Any) -> list[Any]:
    if value is None:
        return []
    if not isinstance(value, list):
        raise ValueError("provider array has an invalid type")
    return value


def _boolean(value: Any) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("provider boolean has an invalid type")
    return value


def _field(record: dict[str, Any], name: str) -> str:
    return _string(record.get(name))


def _instant(name: str, value: str) -> datetime:
    match = _INSTANT.fullmatch(value)
    if match is None:
        raise ValueError(f'{name} "{value}" {_INSTANT_ERROR}')
    try:
        year, month, day = map(int, match[1].split("-"))
        hour, minute, second = map(int, (match[2], match[3], match[4]))
        if hour > 23 or minute > 59 or second > 59:
            raise ValueError
        fraction = int((match[5] or "")[:6].ljust(6, "0"))
        offset = match[6]
        delta = (
            0
            if offset == "Z"
            else (1 if offset[0] == "+" else -1) * (int(offset[1:3]) * 60 + int(offset[4:]))
        )
        if offset != "Z" and int(offset[4:]) > 59:
            raise ValueError
        return (
            datetime(year, month, day, hour, minute, second, fraction) - timedelta(minutes=delta)
        ).replace(tzinfo=UTC)
    except (ValueError, OverflowError) as error:
        raise ValueError(f'{name} "{value}" {_INSTANT_ERROR}') from error


def _date(name: str, value: str) -> date:
    try:
        if not re.fullmatch(r"\d{4}-\d\d-\d\d", value):
            raise ValueError
        return date.fromisoformat(value)
    except ValueError as error:
        raise ValueError(f'{name} "{value}" must use YYYY-MM-DD') from error


def _zone(name: str, value: str) -> None:
    if not value or value != value.strip() or value == "Local":
        raise ValueError(f'{name} "{value}" must be an IANA time zone name such as America/Los_Angeles')
    try:
        ZoneInfo(value)
    except ZoneInfoNotFoundError as error:
        raise ValueError(f'{name} "{value}" is not a known IANA time zone name') from error


def _window(start: str, end: str) -> None:
    if not start or not end:
        raise ValueError("timeMin and timeMax are both required")
    left, right = _instant("timeMin", start), _instant("timeMax", end)
    if right <= left:
        raise ValueError(f"timeMax {end} must be after timeMin {start}")
    if right - left > timedelta(days=366):
        raise ValueError("the timeMin to timeMax window cannot exceed 366 days")


def _boundary(name: str, value: EventDateTime, returned: bool = False) -> None:
    if bool(value.date) == bool(value.date_time):
        raise ValueError(f"{name} must set exactly one of date and dateTime")
    if value.date:
        _date(name + ".date", value.date)
        if value.time_zone and not returned:
            raise ValueError(f"{name} is an all-day date, which has no time zone")
    else:
        _instant(name + ".dateTime", value.date_time)
        if not returned:
            _zone(name + ".timeZone", value.time_zone)


def _event_times(start: EventDateTime, end: EventDateTime, returned: bool = False) -> None:
    _boundary("start", start, returned)
    _boundary("end", end, returned)
    if bool(start.date) != bool(end.date):
        raise ValueError("start and end mix an all-day date with a timed dateTime")
    if not returned:
        if start.date and _date("start.date", start.date) >= _date("end.date", end.date):
            raise ValueError("all-day end date must follow start date")
        if start.date_time and _instant("start.dateTime", start.date_time) >= _instant(
            "end.dateTime", end.date_time
        ):
            raise ValueError("end must be after start")


def _calendar_id(value: str) -> None:
    if not value or value != value.strip():
        raise ValueError("calendarId is required, such as primary or a calendar ID from the calendar picker")


def _event_id(value: str) -> None:
    if not value or value != value.strip():
        raise ValueError("eventId is required")


def _attendees(value: tuple[EventAttendeeInput, ...] | None) -> None:
    seen: set[str] = set()
    for attendee in value or ():
        display, address = parseaddr(attendee.email)
        if not address:
            literal = re.fullmatch(r"(.+)@\[([^\[\]\s]+)\]", attendee.email)
            if literal is not None:
                local_display, local_address = parseaddr(literal[1] + "@example.com")
                if not local_display and local_address == literal[1] + "@example.com":
                    address = attendee.email
        if display or address != attendee.email or "@" not in address:
            raise ValueError(
                f'attendee email "{attendee.email}" must be one bare address such as person@example.com'
            )
        lowered = address.lower()
        if lowered in seen:
            raise ValueError(f'attendee email "{attendee.email}" is duplicated')
        seen.add(lowered)


def _send_updates(value: str) -> None:
    if value not in ("", "none", "all", "externalOnly"):
        raise ValueError("sendUpdates must be none, all, or externalOnly")


def _decoded_boundary(value: Any) -> EventDateTime:
    record = _object(value)
    return EventDateTime(_field(record, "date"), _field(record, "dateTime"), _field(record, "timeZone"))


def _input_boundary(value: Any) -> EventDateTime:
    if isinstance(value, EventDateTime):
        return value
    return _decoded_boundary(value)


def _input_attendees(value: Any) -> tuple[EventAttendeeInput, ...] | None:
    if value is None:
        return None
    return tuple(
        item
        if isinstance(item, EventAttendeeInput)
        else EventAttendeeInput(_field(_object(item), "email"), _boolean(_object(item).get("isOptional")))
        for item in value
    )


def _decode_event(value: Any, calendar_id: str) -> Event:
    record = _object(value)
    event_id = _field(record, "id")
    if not event_id:
        raise ValueError("event response has no ID")
    start = _decoded_boundary(record["start"]) if record.get("start") is not None else EventDateTime()
    end = _decoded_boundary(record["end"]) if record.get("end") is not None else EventDateTime()
    if (
        _field(record, "status") != "cancelled"
        or record.get("start") is not None
        or record.get("end") is not None
    ):
        _event_times(start, end, True)
    guest_list = []
    for value in _array(record.get("attendees")):
        guest = _object(value)
        guest_list.append(
            EventAttendee(
                _field(guest, "email"),
                _field(guest, "displayName"),
                _field(guest, "responseStatus"),
                _boolean(guest.get("optional")),
                _boolean(guest.get("organizer")),
                _boolean(guest.get("self")),
                _boolean(guest.get("resource")),
            )
        )
    conference = None
    if record.get("conferenceData") is not None:
        data = _object(record["conferenceData"])
        solution = _object(data["conferenceSolution"]) if data.get("conferenceSolution") is not None else {}
        key = _object(solution["key"]) if solution.get("key") is not None else {}
        create_request = _object(data["createRequest"]) if data.get("createRequest") is not None else {}
        status = _object(create_request["status"]) if create_request.get("status") is not None else {}
        join_url = ""
        for point in _array(data.get("entryPoints")):
            point = _object(point)
            if _field(point, "entryPointType") == "video":
                join_url = _field(point, "uri")
                break
        conference = EventConference(
            _field(key, "type"), _field(data, "conferenceId"), join_url, _field(status, "statusCode")
        )
    organizer = _object(record["organizer"]) if record.get("organizer") is not None else {}
    original_start = (
        _decoded_boundary(record["originalStartTime"])
        if record.get("originalStartTime") is not None
        else None
    )
    return Event(
        event_id,
        calendar_id,
        _field(record, "status"),
        _field(record, "summary"),
        _field(record, "description"),
        _field(record, "location"),
        start,
        end,
        bool(start.date),
        _field(record, "transparency") or "opaque",
        _field(record, "visibility"),
        tuple(guest_list),
        _field(organizer, "email"),
        _field(record, "recurringEventId"),
        original_start,
        conference,
        _field(record, "htmlLink"),
        _field(record, "iCalUID"),
        _field(record, "etag"),
        _field(record, "created"),
        _field(record, "updated"),
    )


def _json(body: bytes) -> Any:
    try:
        return json.loads(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("provider response is not valid JSON") from error


def _request_failure(
    error: ProviderRequestError, operation: str, mutation: bool, value: Any, receipt: Receipt
) -> Any:
    attempt = MutationAttempt if mutation else QueryAttempt
    if error.kind == FailureKind.RESPONSE_TOO_LARGE:
        return attempt.branch_result(
            "invalidResponse", value, receipt, failure(error.kind, operation, str(error))
        )
    if error.kind == FailureKind.LOCAL_DEFECT:
        return attempt.branch_result("defect", value, Receipt(), failure(error.kind, operation, str(error)))
    kind = FailureKind.TRANSPORT if mutation else FailureKind.AVAILABILITY
    message = "provider request failed" if mutation else "provider is unavailable"
    return attempt.retry(failure(kind, operation, message))


def _status_attempt(
    response: ProviderResponse, operation: str, mutation: bool, value: Any, receipt: Receipt
) -> Any:
    attempt = MutationAttempt if mutation else QueryAttempt
    retry, kind, message, delay = classify_status(response)
    if retry:
        return attempt.retry(failure(kind, operation, message), delay)
    return attempt.branch_result("providerRejected", value, receipt, failure(kind, operation, message))


class ListEventsOperation:
    """List one bounded page of Calendar events."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated listEvents Query definition."""
        return LIST_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: ListEventsInput) -> QueryAttempt[ListEventsOutput]:
        """Issue one events.list request and classify its result."""
        operation = "listEvents"
        empty = ListEventsOutput()
        try:
            _calendar_id(input.calendar_id)
            _window(input.time_min, input.time_max)
            if input.time_zone:
                _zone("timeZone", input.time_zone)
            page_size = input.page_size or 50
            if page_size < 1 or page_size > 250:
                raise ValueError("pageSize must be between 1 and 250, or zero for the default")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials = await self.client.resolve(call, operation)
        if isinstance(credentials, Failure):
            return QueryAttempt.branch_result("defect", empty, Receipt(), credentials)
        query = {
            "singleEvents": "true",
            "orderBy": "startTime",
            "showDeleted": "false",
            "timeMin": input.time_min,
            "timeMax": input.time_max,
            "maxResults": str(page_size),
        }
        query.update(
            {
                key: value
                for key, value in (
                    ("q", input.query),
                    ("pageToken", input.page_token),
                    ("timeZone", input.time_zone),
                )
                if value
            }
        )
        try:
            response = await self.client.send(
                call, credentials, "GET", calendar_path(input.calendar_id) + "/events", query
            )
        except ProviderRequestError as error:
            return _request_failure(error, operation, False, empty, self.client.receipt(call, error.response))
        receipt = self.client.receipt(call, response)
        if response.status in (404, 410):
            return QueryAttempt.branch_result(
                "notFound",
                empty,
                receipt,
                failure(FailureKind.NOT_FOUND, operation, "calendar was not found"),
            )
        if not 200 <= response.status < 300:
            return _status_attempt(response, operation, False, empty, receipt)
        try:
            page = _object(_json(response.body))
            zone = _field(page, "timeZone")
            if zone:
                _zone("timeZone", zone)
            events = []
            for item in _array(page.get("items")):
                event = _decode_event(item, input.calendar_id)
                if event.status != "cancelled":
                    events.append(event)
            output = ListEventsOutput(
                input.calendar_id,
                input.time_min,
                input.time_max,
                zone,
                tuple(events),
                _field(page, "nextPageToken"),
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "provider returned an invalid event page: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("listed", output, receipt)


class GetEventOperation:
    """Read one Calendar event."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated getEvent Query definition."""
        return GET_EVENT_DEFINITION

    async def invoke(self, call: Call, input: GetEventInput) -> QueryAttempt[Event]:
        """Issue one events.get request and classify its result."""
        operation = "getEvent"
        try:
            _calendar_id(input.calendar_id)
            _event_id(input.event_id)
            if input.time_zone:
                _zone("timeZone", input.time_zone)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", Event(), Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials = await self.client.resolve(call, operation)
        if isinstance(credentials, Failure):
            return QueryAttempt.branch_result("defect", Event(), Receipt(), credentials)
        return await _read_event(
            self.client, call, credentials, operation, input.calendar_id, input.event_id, input.time_zone
        )


async def _read_event(
    client: Client,
    call: Call,
    credentials: Any,
    operation: str,
    calendar_id: str,
    event_id: str,
    time_zone: str = "",
) -> QueryAttempt[Event]:
    try:
        response = await client.send(
            call,
            credentials,
            "GET",
            event_path(calendar_id, event_id),
            {"timeZone": time_zone} if time_zone else {},
        )
    except ProviderRequestError as error:
        return _request_failure(error, operation, False, Event(), client.receipt(call, error.response))
    receipt = client.receipt(call, response)
    if response.status in (404, 410):
        return QueryAttempt.branch_result(
            "notFound",
            Event(),
            receipt,
            failure(FailureKind.NOT_FOUND, operation, "calendar or event was not found"),
        )
    if not 200 <= response.status < 300:
        return _status_attempt(response, operation, False, Event(), receipt)
    try:
        event = _decode_event(_json(response.body), calendar_id)
    except ValueError as error:
        return QueryAttempt.branch_result(
            "invalidResponse",
            Event(),
            receipt,
            failure(FailureKind.PROTOCOL, operation, "provider returned an invalid event: " + str(error)),
        )
    return QueryAttempt.branch_result("found", event, client.receipt(call, response, event.id))


def _attendee_payload(attendees: tuple[EventAttendeeInput, ...]) -> list[dict[str, Any]]:
    return [
        {"email": guest.email, **({"optional": True} if guest.is_optional else {})} for guest in attendees
    ]


def _boundary_payload(boundary: EventDateTime) -> dict[str, Any]:
    return {
        "date": boundary.date or None,
        "dateTime": boundary.date_time or None,
        "timeZone": boundary.time_zone or None,
    }


def _read_as_mutation(
    read: QueryAttempt[Event], event_id: str, *, create: bool
) -> MutationAttempt[Any] | None:
    output = CreateEventOutput() if create else UpdateEventOutput()
    if read.kind == "retry":
        assert read.failure is not None
        return MutationAttempt.retry(read.failure, read.retry_after_seconds)
    assert read.receipt is not None
    receipt = Receipt(
        read.receipt.call_id,
        read.receipt.provider,
        read.receipt.observed_at,
        read.receipt.idempotency_key,
        event_id,
        read.receipt.provider_request_id,
    )
    if read.branch == "invalidResponse":
        return MutationAttempt.branch_result("invalidResponse", output, receipt, read.failure)
    if read.branch == "providerRejected":
        return MutationAttempt.branch_result("providerRejected", output, receipt, read.failure)
    if read.branch == "defect":
        return MutationAttempt.branch_result("defect", output, Receipt(), read.failure)
    if read.branch == "notFound" and not create:
        return MutationAttempt.branch_result("notFound", output, receipt, read.failure)
    return None


class CreateEventOperation:
    """Create an event under a stable client event ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated createEvent Mutation definition."""
        return CREATE_EVENT_DEFINITION

    def idempotency_key(self, call_id: str, input: CreateEventInput) -> str:
        """Use the supplied event ID or the Call ID without dashes."""
        return input.event_id or str(call_id).replace("-", "")

    async def invoke(self, call: Call, input: CreateEventInput) -> MutationAttempt[CreateEventOutput]:
        """Read, insert, and read back a duplicate stable event ID."""
        operation = "createEvent"
        empty = CreateEventOutput()
        event_id = str(call.idempotency_key)
        try:
            input = replace(
                input,
                start=_input_boundary(input.start),
                end=_input_boundary(input.end),
                attendees=_input_attendees(input.attendees) or (),
            )
            _calendar_id(input.calendar_id)
            if not _EVENT_ID.fullmatch(event_id):
                raise ValueError("eventId must be 5 to 1024 lowercase base32hex characters (a-v and 0-9)")
            if not input.summary.strip():
                raise ValueError("summary is required")
            _event_times(input.start, input.end)
            _attendees(input.attendees)
            _send_updates(input.send_updates)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials = await self.client.resolve(call, operation)
        if isinstance(credentials, Failure):
            return MutationAttempt.branch_result("defect", empty, Receipt(), credentials)
        read = await _read_event(self.client, call, credentials, operation, input.calendar_id, event_id)
        mapped = _read_as_mutation(read, event_id, create=True)
        if mapped is not None:
            return mapped
        if read.branch == "found":
            assert read.receipt is not None and read.value is not None
            receipt = Receipt(
                read.receipt.call_id,
                read.receipt.provider,
                read.receipt.observed_at,
                read.receipt.idempotency_key,
                event_id,
                read.receipt.provider_request_id,
            )
            if read.value.status == "cancelled":
                return MutationAttempt.branch_result(
                    "conflict",
                    CreateEventOutput(read.value),
                    receipt,
                    failure(FailureKind.CONFLICT, operation, "event ID belongs to a deleted event"),
                )
            return MutationAttempt.branch_result("created", CreateEventOutput(read.value, True), receipt)
        query = {}
        if input.request_conference:
            query["conferenceDataVersion"] = "1"
        if input.send_updates:
            query["sendUpdates"] = input.send_updates
        payload: dict[str, Any] = {
            "id": event_id,
            "summary": input.summary,
            "start": {
                "date": input.start.date,
                "dateTime": input.start.date_time,
                "timeZone": input.start.time_zone,
            },
            "end": {"date": input.end.date, "dateTime": input.end.date_time, "timeZone": input.end.time_zone},
        }
        for boundary in ("start", "end"):
            payload[boundary] = {key: value for key, value in payload[boundary].items() if value}
        if input.description:
            payload["description"] = input.description
        if input.attendees:
            payload["attendees"] = _attendee_payload(input.attendees)
        if input.request_conference:
            payload["conferenceData"] = {
                "createRequest": {"requestId": event_id, "conferenceSolutionKey": {"type": "hangoutsMeet"}}
            }
        try:
            response = await self.client.send(
                call, credentials, "POST", calendar_path(input.calendar_id) + "/events", query, payload
            )
        except ProviderRequestError as error:
            if error.kind == FailureKind.TRANSPORT:
                return MutationAttempt.retry(
                    failure(
                        error.kind,
                        operation,
                        "insert outcome is unknown; the stable event ID makes the retry safe",
                    )
                )
            return _request_failure(
                error, operation, True, empty, self.client.receipt(call, error.response, event_id)
            )
        receipt = self.client.receipt(call, response, event_id)
        if response.status == 409:
            read = await _read_event(self.client, call, credentials, operation, input.calendar_id, event_id)
            if read.branch == "notFound":
                assert read.receipt is not None
                read_receipt = Receipt(
                    read.receipt.call_id,
                    read.receipt.provider,
                    read.receipt.observed_at,
                    read.receipt.idempotency_key,
                    event_id,
                    read.receipt.provider_request_id,
                )
                return MutationAttempt.branch_result(
                    "conflict",
                    empty,
                    read_receipt,
                    failure(
                        FailureKind.CONFLICT,
                        operation,
                        "event ID is used by an event the connection cannot read",
                    ),
                )
            mapped = _read_as_mutation(read, event_id, create=True)
            if mapped is not None:
                return mapped
            assert read.value is not None
            if read.value.status == "cancelled":
                return MutationAttempt.branch_result(
                    "conflict",
                    CreateEventOutput(read.value),
                    receipt,
                    failure(FailureKind.CONFLICT, operation, "event ID belongs to a deleted event"),
                )
            return MutationAttempt.branch_result("created", CreateEventOutput(read.value, True), receipt)
        if not 200 <= response.status < 300:
            return _status_attempt(response, operation, True, empty, receipt)
        try:
            event = _decode_event(_json(response.body), input.calendar_id)
            if event.id != event_id:
                raise ValueError("provider returned an event with another ID")
        except ValueError as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "provider returned an invalid event: " + str(error)),
            )
        return MutationAttempt.branch_result("created", CreateEventOutput(event), receipt)


class UpdateEventOperation:
    """Conditionally patch an existing event after a read and comparison."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated updateEvent Mutation definition."""
        return UPDATE_EVENT_DEFINITION

    def idempotency_key(self, call_id: str, input: UpdateEventInput) -> str:
        """Use the Call ID; read and comparison make the operation repeatable."""
        return str(call_id)

    async def invoke(self, call: Call, input: UpdateEventInput) -> MutationAttempt[UpdateEventOutput]:
        """Read, compare, and conditionally patch one event."""
        operation = "updateEvent"
        empty = UpdateEventOutput()
        try:
            input = replace(
                input,
                start=_input_boundary(input.start) if input.start is not None else None,
                end=_input_boundary(input.end) if input.end is not None else None,
                attendees=_input_attendees(input.attendees),
            )
            _calendar_id(input.calendar_id)
            _event_id(input.event_id)
            if all(
                value is None
                for value in (input.summary, input.description, input.start, input.end, input.attendees)
            ):
                raise ValueError("set at least one of summary, description, start and end, or attendees")
            if input.summary is not None and not input.summary.strip():
                raise ValueError("summary cannot be blank")
            if (input.start is None) != (input.end is None):
                raise ValueError("set start and end together so the event keeps one unambiguous time range")
            if input.start is not None:
                assert input.end is not None
                _event_times(input.start, input.end)
            _attendees(input.attendees)
            _send_updates(input.send_updates)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials = await self.client.resolve(call, operation)
        if isinstance(credentials, Failure):
            return MutationAttempt.branch_result("defect", empty, Receipt(), credentials)
        read = await _read_event(self.client, call, credentials, operation, input.calendar_id, input.event_id)
        mapped = _read_as_mutation(read, input.event_id, create=False)
        if mapped is not None:
            return mapped
        assert read.receipt is not None and read.value is not None
        current = read.value
        receipt = Receipt(
            read.receipt.call_id,
            read.receipt.provider,
            read.receipt.observed_at,
            read.receipt.idempotency_key,
            input.event_id,
            read.receipt.provider_request_id,
        )
        if current.status == "cancelled":
            return MutationAttempt.branch_result(
                "notFound", empty, receipt, failure(FailureKind.NOT_FOUND, operation, "event is cancelled")
            )
        same = (input.summary is None or input.summary == current.summary) and (
            input.description is None or input.description == current.description
        )
        if input.start is not None:
            assert input.end is not None
            same = (
                same and _same_boundary(current.start, input.start) and _same_boundary(current.end, input.end)
            )
        if input.attendees is not None:
            same = same and {(guest.email.lower(), guest.is_optional) for guest in current.attendees} == {
                (guest.email.lower(), guest.is_optional) for guest in input.attendees
            }
        if same:
            return MutationAttempt.branch_result("updated", UpdateEventOutput(current, True), receipt)
        payload: dict[str, Any] = {}
        if input.summary is not None:
            payload["summary"] = input.summary
        if input.description is not None:
            payload["description"] = input.description
        if input.start is not None:
            assert input.end is not None
            payload["start"] = _boundary_payload(input.start)
            payload["end"] = _boundary_payload(input.end)
        if input.attendees is not None:
            payload["attendees"] = _attendee_payload(input.attendees)
        try:
            response = await self.client.send(
                call,
                credentials,
                "PATCH",
                event_path(input.calendar_id, input.event_id),
                {"sendUpdates": input.send_updates} if input.send_updates else {},
                payload,
                current.etag,
            )
        except ProviderRequestError as error:
            if error.kind == FailureKind.TRANSPORT:
                return MutationAttempt.retry(
                    failure(
                        error.kind, operation, "patch outcome is unknown; the retry rereads the event first"
                    )
                )
            return _request_failure(
                error, operation, True, empty, self.client.receipt(call, error.response, input.event_id)
            )
        receipt = self.client.receipt(call, response, input.event_id)
        if response.status == 412:
            return MutationAttempt.retry(
                failure(FailureKind.CONFLICT, operation, "event changed after it was read")
            )
        if response.status in (404, 410):
            return MutationAttempt.branch_result(
                "notFound",
                empty,
                receipt,
                failure(FailureKind.NOT_FOUND, operation, "calendar or event was not found"),
            )
        if not 200 <= response.status < 300:
            return _status_attempt(response, operation, True, empty, receipt)
        try:
            event = _decode_event(_json(response.body), input.calendar_id)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(FailureKind.PROTOCOL, operation, "provider returned an invalid event: " + str(error)),
            )
        return MutationAttempt.branch_result("updated", UpdateEventOutput(event), receipt)


def _same_boundary(current: EventDateTime, requested: EventDateTime) -> bool:
    if current.date or requested.date:
        return bool(current.date) == bool(requested.date) and current.date == requested.date
    return (
        _instant("dateTime", current.date_time) == _instant("dateTime", requested.date_time)
        and current.time_zone == requested.time_zone
    )


class QueryFreeBusyOperation:
    """Query ordered busy intervals for one or more calendars."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the generated queryFreeBusy Query definition."""
        return QUERY_FREE_BUSY_DEFINITION

    async def invoke(self, call: Call, input: QueryFreeBusyInput) -> QueryAttempt[QueryFreeBusyOutput]:
        """Issue one freeBusy.query request and classify incomplete responses."""
        operation = "queryFreeBusy"
        empty = QueryFreeBusyOutput()
        try:
            if not 1 <= len(input.calendar_ids) <= 50:
                raise ValueError("calendarIds must list 1 to 50 calendars")
            for calendar_id in input.calendar_ids:
                _calendar_id(calendar_id)
                if input.calendar_ids.count(calendar_id) > 1:
                    raise ValueError(f'calendar ID "{calendar_id}" is duplicated')
            _window(input.time_min, input.time_max)
            if input.time_zone:
                _zone("timeZone", input.time_zone)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, str(error))
            )
        credentials = await self.client.resolve(call, operation)
        if isinstance(credentials, Failure):
            return QueryAttempt.branch_result("defect", empty, Receipt(), credentials)
        payload = {
            "timeMin": input.time_min,
            "timeMax": input.time_max,
            "items": [{"id": name} for name in input.calendar_ids],
        }
        if input.time_zone:
            payload["timeZone"] = input.time_zone
        try:
            response = await self.client.send(call, credentials, "POST", "/freeBusy", payload=payload)
        except ProviderRequestError as error:
            return _request_failure(error, operation, False, empty, self.client.receipt(call, error.response))
        receipt = self.client.receipt(call, response)
        if not 200 <= response.status < 300:
            return _status_attempt(response, operation, False, empty, receipt)
        try:
            record = _object(_json(response.body))
            calendar_map = _object(record.get("calendars"))
            calendars = []
            for calendar_id in input.calendar_ids:
                value = calendar_map.get(calendar_id)
                if value is None:
                    value = next(
                        (item for name, item in calendar_map.items() if name.lower() == calendar_id.lower()),
                        None,
                    )
                if value is None:
                    raise ValueError(f'calendar "{calendar_id}" is missing from the response')
                entry = _object(value)
                intervals = []
                for item in _array(entry.get("busy")):
                    item = _object(item)
                    start = _field(item, "start")
                    end = _field(item, "end")
                    if _instant("busy.start", start) >= _instant("busy.end", end):
                        raise ValueError(
                            f'calendar "{calendar_id}" has a busy interval without two ordered '
                            "RFC 3339 offsets"
                        )
                    intervals.append(BusyInterval(start, end))
                errors = tuple(
                    FreeBusyError(_field(_object(item), "domain"), _field(_object(item), "reason"))
                    for item in _array(entry.get("errors"))
                )
                calendars.append(CalendarFreeBusy(calendar_id, tuple(intervals), errors))
            output = QueryFreeBusyOutput(
                _field(record, "timeMin") or input.time_min,
                _field(record, "timeMax") or input.time_max,
                input.time_zone or "UTC",
                tuple(calendars),
            )
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "provider returned an invalid free/busy response: " + str(error),
                ),
            )
        if any(calendar.errors for calendar in output.calendars):
            return QueryAttempt.branch_result(
                "incomplete",
                output,
                receipt,
                failure(
                    FailureKind.PROVIDER_REJECTION,
                    operation,
                    "provider could not report availability for every calendar",
                ),
            )
        return QueryAttempt.branch_result("queried", output, receipt)
