# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""recordModifiedPolled configuration, change kinds, event IDs, and the cursor, at parity with Go."""

import hashlib
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta, timezone
from typing import Final

from .client import is_api_name, is_record_id
from .models import Record, RecordModifiedTriggerConfiguration

MAX_TRIGGER_FIELD_NAMES: Final = 50
"""Bounds the extra fields one recordModifiedPolled binding reads with each record."""
RECORD_CHANGE_CREATED: Final = "created"
"""A record whose LastModifiedDate still equals its CreatedDate."""
RECORD_CHANGE_UPDATED: Final = "updated"
"""Any other change that moved SystemModstamp, including a system change that left LastModifiedDate."""
TRIGGER_SYSTEM_FIELDS: Final = ("Id", "CreatedDate", "LastModifiedDate", "SystemModstamp")
"""Fields selected for every record; they order the poll and derive the change kind."""

# The REST API's 2026-03-02T09:00:00.000+0000 form and RFC 3339, as Go's three accepted layouts.
_SALESFORCE_DATETIME: Final = re.compile(
    r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2}|[+-]\d{4})\Z", re.ASCII
)
_RFC3339: Final = re.compile(
    r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})\Z", re.ASCII
)
_CURSOR_FIELDS: Final = ("sObjectType", "systemModstamp", "recordId")


@dataclass(frozen=True, slots=True)
class Instant:
    """A UTC time with Go's nanosecond precision; Python datetimes keep only microseconds."""

    moment: datetime
    """The time truncated to whole microseconds, in UTC."""
    nanoseconds: int = 0
    """The nanoseconds below the microsecond."""

    def is_whole_second(self) -> bool:
        """Report whether no fraction of a second is set."""
        return self.moment.microsecond == 0 and self.nanoseconds == 0

    def key(self) -> tuple[datetime, int]:
        """Return the comparison key."""
        return self.moment, self.nanoseconds


def _parse(match: re.Match[str] | None) -> Instant | None:
    if match is None:
        return None
    year, month, day, hour, minute, second = (int(part) for part in match.groups()[:6])
    fraction, zone = match.group(7) or "", match.group(8)
    offset = timedelta(0)
    if zone != "Z":
        digits = zone[1:].replace(":", "")
        hours, minutes = int(digits[:2]), int(digits[2:])
        if hours >= 24 or minutes >= 60:
            return None
        offset = timedelta(hours=hours, minutes=minutes) * (-1 if zone[0] == "-" else 1)
    nanoseconds = int(fraction[:9].ljust(9, "0"))
    try:
        local = datetime(year, month, day, hour, minute, second, nanoseconds // 1000, timezone(offset))
        moment = local.astimezone(UTC)
    except (ValueError, OverflowError):
        return None
    return Instant(moment, nanoseconds % 1000)


def parse_salesforce_datetime(value: str) -> Instant | None:
    """Parse the REST API's dateTime form or RFC 3339, as the Go connector does, or return None."""
    match = _SALESFORCE_DATETIME.fullmatch(value)
    if match is not None and match.group(8)[0] in "+-" and ":" not in match.group(8):
        # Go's two REST layouts take exactly three fraction digits before a +hhmm offset.
        if len(match.group(7) or "") != 3:
            return None
    return _parse(match)


def parse_rfc3339(value: str) -> Instant | None:
    """Parse value as Go's time.Parse(time.RFC3339) does, or return None."""
    return _parse(_RFC3339.fullmatch(value))


def format_rfc3339(moment: datetime) -> str:
    """Format a whole-second UTC time as Go's time.RFC3339 does."""
    return moment.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")


def validate_record_modified_trigger_configuration(configuration: RecordModifiedTriggerConfiguration) -> None:
    """Require an object API name and reject invalid or repeated field API names; raises ValueError."""
    if not isinstance(configuration.s_object_type, str) or not is_api_name(configuration.s_object_type):
        raise ValueError("recordModifiedPolled sObjectType must be an object API name such as Opportunity")
    if len(configuration.field_names) > MAX_TRIGGER_FIELD_NAMES:
        raise ValueError(
            f"recordModifiedPolled fieldNames must hold at most {MAX_TRIGGER_FIELD_NAMES} field API names"
        )
    seen: set[str] = set()
    for field_name in configuration.field_names:
        if not isinstance(field_name, str) or not is_api_name(field_name):
            raise ValueError(
                "recordModifiedPolled fieldNames must be field API names such as StageName, "
                "without relationship dots"
            )
        if field_name.lower() in seen:
            raise ValueError("recordModifiedPolled fieldNames must not repeat a field API name")
        seen.add(field_name.lower())


def selected_fields(configuration: RecordModifiedTriggerConfiguration) -> list[str]:
    """Return the system fields followed by each configured field that is not one of them."""
    system = {name.lower() for name in TRIGGER_SYSTEM_FIELDS}
    return [
        *TRIGGER_SYSTEM_FIELDS,
        *(name for name in configuration.field_names if name.lower() not in system),
    ]


def string_field(record: Record, field_name: str) -> str | None:
    """Return a field's text when it holds a JSON string; names match exactly, else without case."""
    fields = record.fields or {}
    if field_name in fields:
        value = fields[field_name]
    else:
        value = next((item for name, item in fields.items() if name.lower() == field_name.lower()), None)
    return value if isinstance(value, str) else None


def record_change_kind(record: Record) -> str:
    """Compare CreatedDate with LastModifiedDate; an unreadable pair counts as updated."""
    created_text, modified_text = (
        string_field(record, "CreatedDate"),
        string_field(record, "LastModifiedDate"),
    )
    if created_text is None or modified_text is None:
        return RECORD_CHANGE_UPDATED
    created, modified = parse_salesforce_datetime(created_text), parse_salesforce_datetime(modified_text)
    if created is not None and modified is not None and created.key() == modified.key():
        return RECORD_CHANGE_CREATED
    return RECORD_CHANGE_UPDATED


def record_modified_event_id(s_object_type: str, record_id: str, modstamp: datetime) -> str:
    """Return the Trigger event ID, a pure function of the record and its SystemModstamp, as Go does."""
    identity = "\x00".join((s_object_type, record_id, format_rfc3339(modstamp)))
    return "sfdc-" + hashlib.sha256(identity.encode()).digest()[:16].hex()


def is_same_record_id(left: str, right: str) -> bool:
    """Compare IDs by their case-sensitive 15-character prefix, which both ID forms share."""
    return is_record_id(left) and is_record_id(right) and left[:15] == right[:15]
