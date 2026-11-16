# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Read BambooHR's changed-employees list for the employeeChanged poll Trigger.

The cursor token is the Go connector's document byte for byte, so Go and Python replicas share one
binding cursor.
"""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta, timezone
from typing import Any, Final

import httpx
from dex_connectors.sdk import ConnectionRef, FailureKind, TriggerContext, TriggerEvent
from dex_connectors.sdk.pollingtrigger import (
    MAXIMUM_CHANGES_PER_PAGE,
    ChangePage,
    Cursor,
    mark_cursor_invalid,
    mark_rate_limited,
    new_trigger_credential_call,
    wait_for_poll_request,
)
from dex_connectors.sdk.projectconfig import encode_go_json_value

from ._generated.definitions import EMPLOYEE_CHANGED_TRIGGER_DEFINITION
from .client import Client, Exchange
from .models import EmployeeChangedEvent, EmployeeChangedTriggerConfiguration

EMPLOYEE_CHANGE_TYPES: Final = ("inserted", "updated", "deleted")
"""The change types a binding may keep; blank delivers every change."""
EMPLOYEE_CHANGE_ACTION_INSERTED: Final = "Inserted"
"""BambooHR's action for an added employee."""
EMPLOYEE_CHANGE_ACTION_UPDATED: Final = "Updated"
"""BambooHR's action for a changed employee, including a termination."""
EMPLOYEE_CHANGE_ACTION_DELETED: Final = "Deleted"
"""BambooHR's action for a deleted employee record."""
EMPLOYEE_CHANGE_SETTLE_WINDOW: Final = timedelta(seconds=30)
"""How far behind BambooHR's Date header a change must be before it is delivered.

It covers changes that become visible after a later second was already listed and any difference between
the clock that stamps lastChanged and the one that sends Date.
"""

_TRIGGER_NAME: Final = "employeeChanged"
_CURSOR_VERSION: Final = 1
_CHANGE_QUERY_OVERLAP: Final = timedelta(seconds=1)
_EMPLOYEE_ID = re.compile(r"[1-9][0-9]{0,18}", re.ASCII)
_ACTION = re.compile(r"[A-Za-z]{1,32}", re.ASCII)
_RFC3339 = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})(?:[.,]([0-9]+))?"
    r"(Z|([+-])([0-9]{2}):([0-9]{2}))",
    re.ASCII,
)
_MONTHS: Final = ("Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec")
_IMF_FIXDATE = re.compile(
    r"(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun), ([0-9]{2}) ([A-Z][a-z]{2}) ([0-9]{4}) "
    r"([0-9]{2}):([0-9]{2}):([0-9]{2}) GMT",
    re.ASCII,
)
_ZERO_TIME: Final = datetime(1, 1, 1, tzinfo=UTC)
_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


class EmployeeChangedCursorInvalidError(ValueError):
    """An employeeChanged cursor token that is not a valid Go cursor document for this binding."""

    def __init__(self, message: str = "BambooHR employeeChanged cursor is not decodable") -> None:
        """Describe the rejected token without echoing it."""
        super().__init__(message)


@dataclass(frozen=True, slots=True)
class _Change:
    employee_id: str
    action: str
    last_changed: datetime


@dataclass(frozen=True, slots=True)
class _Position:
    since: datetime
    after_employee_id: str = ""
    change_type: str = ""


def validate_employee_changed_trigger_configuration(
    configuration: EmployeeChangedTriggerConfiguration,
) -> None:
    """Reject a change type other than inserted, updated, deleted, or blank with ValueError."""
    change_type = configuration.change_type or ""
    if change_type and change_type not in EMPLOYEE_CHANGE_TYPES:
        raise ValueError(
            "bamboohr employeeChanged configuration: BambooHR employeeChanged changeType must be inserted, "
            f"updated, deleted, or blank, not {_go_quote(change_type)}"
        )


def employee_changed_event_id(employee_id: str, action: str, last_changed: datetime) -> str:
    """Return the Trigger event ID of one change: employee-<id>-<action>-<lastChanged Unix seconds>.

    It is a pure function of the employee, action, and change instant, so a refetch after a crash yields
    the same ID and a later change to the same employee yields a new one.
    """
    seconds = (last_changed - _EPOCH) // timedelta(seconds=1)
    return f"employee-{employee_id}-{action.lower()}-{seconds}"


def format_go_time(moment: datetime) -> str:
    """Format moment as Go's time.Time JSON encoding does: RFC 3339 with trailing fraction zeros trimmed.

    A zero offset prints Z; another offset prints as +hh:mm, as Go keeps a parsed time's offset.
    """
    offset = moment.utcoffset() or timedelta(0)
    text = (
        f"{moment.year:04d}-{moment.month:02d}-{moment.day:02d}T"
        f"{moment.hour:02d}:{moment.minute:02d}:{moment.second:02d}"
    )
    if moment.microsecond:
        text += "." + f"{moment.microsecond:06d}".rstrip("0")
    if not offset:
        return text + "Z"
    minutes = int(abs(offset) // timedelta(minutes=1))
    sign = "-" if offset < timedelta(0) else "+"
    return f"{text}{sign}{minutes // 60:02d}:{minutes % 60:02d}"


def parse_go_time(value: str) -> datetime:
    """Parse value as Go's RFC 3339 time parsing does, keeping its offset; fractions past microseconds drop.

    As in Go, T and Z are uppercase, a comma may separate the fraction, and an offset may reach 24:60; an
    offset Python cannot keep converts the time to UTC. Raises ValueError for any other text.
    """
    match = _RFC3339.fullmatch(value)
    if match is None:
        raise ValueError("not an RFC 3339 time")
    year, month, day, hour, minute, second, fraction, zone, sign, zone_hour, zone_minute = match.groups()
    offset = timedelta(0)
    if zone != "Z":
        if int(zone_hour) > 24 or int(zone_minute) > 60:
            raise ValueError("time zone offset out of range")
        offset = timedelta(hours=int(zone_hour), minutes=int(zone_minute)) * (1 if sign == "+" else -1)
    try:
        local = datetime(
            int(year),
            int(month),
            int(day),
            int(hour),
            int(minute),
            int(second),
            int((fraction or "")[:6].ljust(6, "0")),
            tzinfo=UTC,
        )
        if not offset:
            return local
        if abs(offset) < timedelta(hours=24):
            return local.replace(tzinfo=timezone(offset))
        return local - offset
    except (ValueError, OverflowError) as error:
        raise ValueError("RFC 3339 time out of range") from error


def _go_quote(value: str) -> str:
    return json.dumps(value, ensure_ascii=False)


def _compare_employee_ids(left: str, right: str) -> int:
    if len(left) != len(right):
        return len(left) - len(right)
    return (left > right) - (left < right)


def _go_object(value: Any, names: tuple[str, ...]) -> dict[str, Any]:
    """Select names from decoded JSON object pairs as Go fills a struct: case-insensitive, last one wins."""
    folded = {name.casefold(): name for name in names}
    selected: dict[str, Any] = {}
    for key, item in value:
        name = folded.get(key.casefold())
        if name is not None:
            selected[name] = item
    return selected


def _load_pairs(body: bytes | str) -> Any:
    return json.loads(body, object_pairs_hook=list, parse_constant=_reject_constant)


def _reject_constant(constant: str) -> None:
    raise ValueError(f"invalid JSON number: {constant}")


def _is_object(value: Any) -> bool:
    return isinstance(value, list) and all(isinstance(member, tuple) for member in value)


def _decode_employee_id(value: Any) -> str:
    if type(value) is int:
        value = str(value)
    if not isinstance(value, str) or _EMPLOYEE_ID.fullmatch(value) is None:
        raise ValueError("a change names an invalid employee ID")
    return value


def _decode_employee_changes(body: bytes) -> list[_Change]:
    """Decode BambooHR's changed-employees map as the Go connector does; raises ValueError."""
    try:
        # Go rejects a byte order mark and replaces invalid UTF-8; json.loads(bytes) would strip the mark.
        document = _load_pairs(body.decode("utf-8", errors="replace"))
    except (ValueError, RecursionError):
        raise ValueError("the response is not a change list") from None
    if document is not None and not _is_object(document):
        raise ValueError("the response is not a change list")
    employees = _go_object(document or [], ("employees",)).get("employees", _ABSENT)
    if employees is _ABSENT:
        raise ValueError("the response is not a change list")
    # A PHP encoder writes an empty map as [], which is how a period without changes may arrive.
    if employees == []:
        return []
    if not _is_object(employees):
        raise ValueError("employees is not a map of changes")
    entries: dict[str, Any] = {}
    for key, entry in employees:
        if entry is not None and not _is_object(entry):
            raise ValueError("employees is not a map of changes")
        fields = _go_object(entry or [], ("id", "action", "lastChanged"))
        for name in ("action", "lastChanged"):
            if fields.get(name) is not None and not isinstance(fields[name], str):
                raise ValueError("employees is not a map of changes")
        entries[key] = fields
    changes: list[_Change] = []
    for employee_id, fields in entries.items():
        if "id" in fields and _decode_employee_id(fields["id"]) != employee_id:
            raise ValueError("a change names an invalid employee ID")
        _decode_employee_id(employee_id)
        action = fields.get("action") or ""
        if _ACTION.fullmatch(action) is None:
            raise ValueError(f"employee {employee_id} has an invalid action")
        try:
            last_changed = parse_go_time(fields.get("lastChanged") or "").astimezone(UTC)
        except (ValueError, OverflowError):
            raise ValueError(f"employee {employee_id} has an invalid lastChanged") from None
        changes.append(_Change(employee_id, action, last_changed))
    return changes


_ABSENT: Final = object()


def _is_after_cursor(change: _Change, position: _Position) -> bool:
    if change.last_changed != position.since:
        return change.last_changed > position.since
    return not position.after_employee_id or (
        _compare_employee_ids(change.employee_id, position.after_employee_id) > 0
    )


def _page_employee_changes(
    changes: list[_Change], position: _Position
) -> tuple[list[_Change], _Position, bool]:
    """Keep the changes after position, oldest first, and return at most one Trigger page of them."""
    after = [change for change in changes if _is_after_cursor(change, position)]
    after.sort(key=lambda change: (change.last_changed, len(change.employee_id), change.employee_id))
    has_more = len(after) > MAXIMUM_CHANGES_PER_PAGE
    page = after[:MAXIMUM_CHANGES_PER_PAGE]
    next_position = position
    if page:
        next_position = _Position(page[-1].last_changed, page[-1].employee_id, position.change_type)
    return page, next_position, has_more


def _encode_cursor(position: _Position) -> Cursor:
    """Encode the position as Go's json.Marshal does: struct field order, omitempty, HTML-safe strings."""
    members = [
        f'"v":{_CURSOR_VERSION}',
        '"since":' + encode_go_json_value(format_go_time(position.since)).decode(),
    ]
    if position.after_employee_id:
        members.append('"afterEmployeeId":' + encode_go_json_value(position.after_employee_id).decode())
    if position.change_type:
        members.append('"changeType":' + encode_go_json_value(position.change_type).decode())
    return Cursor(token="{" + ",".join(members) + "}")


def _decode_cursor(token: str) -> _Position:
    """Decode token as Go's json.Decoder with DisallowUnknownFields fills the cursor struct.

    Keys match case-insensitively, the last duplicate wins, null leaves the zero value, unknown keys and
    values of the wrong JSON type reject the token, and text after the first JSON value is ignored.
    """
    invalid = EmployeeChangedCursorInvalidError
    try:
        text = token.lstrip(" \t\r\n")
        members, _ = json.JSONDecoder(object_pairs_hook=list, parse_constant=_reject_constant).raw_decode(
            text
        )
    except (ValueError, RecursionError):
        raise invalid() from None
    if members is None:
        raise invalid()
    if not _is_object(members):
        raise invalid()
    known = {"v": "v", "since": "since", "afteremployeeid": "afterEmployeeId", "changetype": "changeType"}
    fields: dict[str, Any] = {"v": 0, "since": None, "afterEmployeeId": "", "changeType": ""}
    for key, value in members:
        name = known.get(key.casefold())
        if name is None:
            raise invalid()
        if value is None:
            continue
        if name == "v":
            if type(value) is not int:
                raise invalid()
        elif not isinstance(value, str):
            raise invalid()
        fields[name] = value
    if fields["v"] != _CURSOR_VERSION or fields["since"] is None:
        raise invalid()
    try:
        since = parse_go_time(fields["since"])
        if since.astimezone(UTC) == _ZERO_TIME:
            raise invalid()
    except (ValueError, OverflowError):
        raise invalid() from None
    after_employee_id = fields["afterEmployeeId"]
    if after_employee_id and _EMPLOYEE_ID.fullmatch(after_employee_id) is None:
        raise invalid()
    change_type = fields["changeType"]
    if change_type and change_type not in EMPLOYEE_CHANGE_TYPES:
        raise invalid()
    return _Position(since, after_employee_id, change_type)


_RFC850 = re.compile(
    r"(?:Monday|Tuesday|Wednesday|Thursday|Friday|Saturday|Sunday), ([0-9]{2})-([A-Za-z]{3})-([0-9]{2}) "
    r"([0-9]{2}):([0-9]{2}):([0-9]{2}) (?:GMT|UTC)",
    re.ASCII,
)
_ANSI_C = re.compile(
    r"(?:Mon|Tue|Wed|Thu|Fri|Sat|Sun) ([A-Za-z]{3}) ([ 0-9][0-9]) "
    r"([0-9]{2}):([0-9]{2}):([0-9]{2}) ([0-9]{4})",
    re.ASCII,
)


def parse_http_date(value: str) -> datetime | None:
    """Parse an HTTP Date header in the three formats Go's http.ParseTime accepts; None otherwise."""
    month_numbers = {name.casefold(): index + 1 for index, name in enumerate(_MONTHS)}
    try:
        if match := _IMF_FIXDATE.fullmatch(value):
            day, month, year, hour, minute, second = match.groups()
        elif match := _RFC850.fullmatch(value):
            day, month, short_year, hour, minute, second = match.groups()
            year = str((1900 if int(short_year) >= 69 else 2000) + int(short_year))
        elif match := _ANSI_C.fullmatch(value):
            month, day, hour, minute, second, year = match.groups()
        else:
            return None
        number = month_numbers.get(month.casefold())
        if number is None:
            return None
        return datetime(int(year), number, int(day), int(hour), int(minute), int(second), tzinfo=UTC)
    except ValueError:
        return None


class EmployeeChangeFetcher:
    """Read one binding's BambooHR changed-employees list for the poll Trigger kit.

    It implements `ChangeFetcher[TriggerEvent[EmployeeChangedEvent]]` at parity with the Go connector:
    the cursor is the last delivered change's lastChanged and employee ID plus the change type it was read
    with. A poller owns one fetcher and calls it from one task at a time.
    """

    def __init__(
        self, client: Client, connection: ConnectionRef, configuration: EmployeeChangedTriggerConfiguration
    ) -> None:
        """Bind the fetcher to one connection's client and one binding's change type."""
        if client is None or connection is None or configuration is None:
            raise ValueError("BambooHR employeeChanged client, connection, and configuration are required")
        self._client = client
        self._change_type = configuration.change_type or ""
        self._call = new_trigger_credential_call(connection, EMPLOYEE_CHANGED_TRIGGER_DEFINITION.trigger)

    async def seed_cursor(self, context: TriggerContext) -> Cursor:
        """Start the cursor at BambooHR's clock from the response Date header, or the local clock.

        No change before that second is delivered; a change BambooHR stamps with that second itself may
        be, because it may have been made after the binding started. The request also proves the API key
        works.
        """
        response = await self._list_changed_employees(context, self._client.now())
        try:
            _decode_employee_changes(response.body)
        except ValueError as error:
            raise RuntimeError(f"BambooHR returned an invalid change list: {error}") from None
        return _encode_cursor(_Position(self._bamboohr_time(response), "", self._change_type))

    async def fetch_changes(
        self, context: TriggerContext, cursor: Cursor
    ) -> ChangePage[TriggerEvent[EmployeeChangedEvent]]:
        """Return the oldest 500 settled changes after cursor, ordered by lastChanged then employee ID.

        Only changes at least `EMPLOYEE_CHANGE_SETTLE_WINDOW` older than BambooHR's Date header are
        delivered; newer ones wait for a later poll. An undecodable cursor, one read with another change
        type, or a 400 raises `mark_cursor_invalid`; a 429 or 503 raises `mark_rate_limited`.
        """
        try:
            position = _decode_cursor(cursor.token)
        except EmployeeChangedCursorInvalidError as error:
            raise mark_cursor_invalid(error) from None
        if position.change_type != self._change_type:
            raise mark_cursor_invalid(
                EmployeeChangedCursorInvalidError(
                    "BambooHR employeeChanged cursor was read with another changeType"
                )
            )
        response = await self._list_changed_employees(context, position.since)
        try:
            changes = _decode_employee_changes(response.body)
        except ValueError as error:
            raise RuntimeError(f"BambooHR returned an invalid change list: {error}") from None
        settled_through = self._bamboohr_time(response) - EMPLOYEE_CHANGE_SETTLE_WINDOW
        settled = [change for change in changes if change.last_changed <= settled_through]
        page, next_position, has_more = _page_employee_changes(settled, position)
        events = tuple(
            TriggerEvent(
                employee_changed_event_id(change.employee_id, change.action, change.last_changed),
                change.last_changed,
                EmployeeChangedEvent(
                    change.employee_id, action=change.action, last_changed=change.last_changed
                ),
            )
            for change in page
        )
        return ChangePage(events, _encode_cursor(next_position), has_more)

    async def _list_changed_employees(self, context: TriggerContext, since: datetime) -> Exchange:
        """Send GET /employees/changed one second before since, as listEmployeeChanges does."""
        await wait_for_poll_request(context)
        credentials, failure = await self._client.resolve_credentials(self._call, _TRIGGER_NAME)
        if failure is not None or credentials is None:
            message = failure.message if failure is not None else "connection credentials are unavailable"
            raise RuntimeError(f"BambooHR employeeChanged poll: {message}")
        query_instant = (since.astimezone(UTC) - _CHANGE_QUERY_OVERLAP).replace(microsecond=0)
        query = {"since": format_go_time(query_instant)}
        if self._change_type:
            query["type"] = self._change_type
        exchange = await self._client.exchange(
            self._call, credentials, _TRIGGER_NAME, "GET", "/employees/changed", query
        )
        message = (
            exchange.failure.message if exchange.failure is not None else "BambooHR returned no response"
        )
        if exchange.outcome == "succeeded":
            return exchange
        if exchange.outcome == "rate_limited" or exchange.status_code == 503:
            # BambooHR throttles per company and API key, so a 429 or 503 pauses every binding.
            raise mark_rate_limited(RuntimeError(message), timedelta(seconds=exchange.retry_after))
        if exchange.status_code == 400:
            raise mark_cursor_invalid(RuntimeError(message))
        if exchange.failure is not None and exchange.failure.kind == FailureKind.RESPONSE_TOO_LARGE:
            raise RuntimeError(
                "BambooHR changed-employees list exceeds maxResponseBytes; raise maxResponseBytes on the "
                "connection"
            )
        raise RuntimeError(f"BambooHR employeeChanged poll: {message}")

    def _bamboohr_time(self, exchange: Exchange) -> datetime:
        """Return BambooHR's clock from the Date header, or the local clock, in whole UTC seconds."""
        headers = exchange.headers if exchange.headers is not None else httpx.Headers()
        server_time = parse_http_date(headers.get("Date", ""))
        if server_time is not None:
            return server_time
        return self._client.now().astimezone(UTC).replace(microsecond=0)
