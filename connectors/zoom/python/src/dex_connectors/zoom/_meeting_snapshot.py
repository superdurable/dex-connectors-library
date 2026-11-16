# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The meetingChanged keyed snapshot entries, change kinds, and event identities, byte-compatible with Go."""

import hashlib
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from enum import StrEnum
from typing import Final

from dex_connectors.sdk import TriggerEvent
from dex_connectors.sdk.projectconfig import KeyedSnapshotEntry

from .models import MeetingChangedEvent, MeetingChangedTriggerConfiguration, MeetingSummary

_GO_HTML_ESCAPES: Final = (
    ("<", "\\u003c"),
    (">", "\\u003e"),
    ("&", "\\u0026"),
    ("\u2028", "\\u2028"),
    ("\u2029", "\\u2029"),
)
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
_UNIX_EPOCH: Final = datetime(1970, 1, 1, tzinfo=UTC)


class MeetingChangeKind(StrEnum):
    """The kind of one meetingChanged delivery.

    Zoom's meeting list reports no change type or modification time, so the kind comes from comparing
    two polls of the list.
    """

    SCHEDULED = "scheduled"
    """A meeting the previous poll did not list, such as a newly scheduled meeting."""
    UPDATED = "updated"
    """A listed meeting whose topic, type, start, duration, time zone, or listed agenda changed.

    A recurring meeting whose listed start moves to its next occurrence also arrives as updated.
    """
    REMOVED = "removed"
    """A meeting the list no longer contains: it was deleted, or it expired after its scheduled time."""


def validate_meeting_changed_trigger_configuration(configuration: MeetingChangedTriggerConfiguration) -> None:
    """Reject an unknown or repeated change kind with ValueError; empty change kinds deliver every kind."""
    seen: set[str] = set()
    for kind in configuration.change_kinds:
        if kind not in tuple(MeetingChangeKind):
            raise ValueError(
                f"meetingChanged changeKinds entry {_go_json(kind)} must be scheduled, updated, or removed"
            )
        if kind in seen:
            raise ValueError(f"meetingChanged changeKinds repeats {_go_json(kind)}")
        seen.add(kind)


def delivers_meeting_change(configuration: MeetingChangedTriggerConfiguration, kind: str) -> bool:
    """Report whether the configuration delivers kind."""
    return not configuration.change_kinds or kind in configuration.change_kinds


def _go_json(value: object) -> str:
    """Encode value as Go's encoding/json does for strings, numbers, and ordered objects."""
    if isinstance(value, str):
        value = _LONE_SURROGATE.sub("�", value)
    serialized = json.dumps(value, separators=(",", ":"), ensure_ascii=False)
    for character, escape in _GO_HTML_ESCAPES:
        serialized = serialized.replace(character, escape)
    return serialized


def _go_string(value: str) -> str:
    return _LONE_SURROGATE.sub("�", value)


def _instant_nanoseconds(timestamp: str) -> int:
    """Return nanoseconds since the Unix epoch of a canonical UTC timestamp, such as 2036-02-24T03:30:00Z."""
    whole, _, fraction = timestamp.removesuffix("Z").partition(".")
    instant = datetime.fromisoformat(whole).replace(tzinfo=UTC)
    seconds = (instant - _UNIX_EPOCH) // timedelta(seconds=1)
    return seconds * 1_000_000_000 + int(fraction.ljust(9, "0")[:9] or "0")


def _timestamp_datetime(timestamp: str) -> datetime:
    """Return a canonical UTC timestamp as an aware datetime, truncated to microseconds."""
    whole, _, fraction = timestamp.removesuffix("Z").partition(".")
    microseconds = int(fraction.ljust(6, "0")[:6] or "0")
    return datetime.fromisoformat(whole).replace(tzinfo=UTC, microsecond=microseconds)


def _tracked_meeting_json(summary: MeetingSummary) -> bytes:
    """Encode the tracked fields as Go marshals trackedMeeting; it never keeps the UUID or join URL."""
    fields: list[tuple[str, object]] = [("id", summary.id)]
    if summary.host_id:
        fields.append(("hostId", summary.host_id))
    fields.append(("topic", summary.topic))
    if summary.type:
        fields.append(("type", summary.type))
    if summary.start_time is not None:
        fields.append(("startTime", summary.start_time))
    if summary.duration_minutes:
        fields.append(("durationMinutes", summary.duration_minutes))
    if summary.time_zone:
        fields.append(("timeZone", summary.time_zone))
    if summary.agenda:
        fields.append(("agenda", summary.agenda))
    if summary.created_at is not None:
        fields.append(("createdAt", summary.created_at))
    encoded = ",".join(f"{_go_json(name)}:{_go_json(value)}" for name, value in fields)
    return ("{" + encoded + "}").encode("utf-8")


@dataclass(frozen=True, slots=True)
class ListedMeeting:
    """One listed meeting adapted to the kit's keyed snapshot."""

    summary: MeetingSummary
    value: bytes

    @classmethod
    def from_summary(cls, summary: MeetingSummary) -> "ListedMeeting":
        """Keep the summary beside its tracked JSON."""
        return cls(summary, _tracked_meeting_json(summary))

    def snapshot_key(self) -> str:
        """The decimal meeting ID."""
        return str(self.summary.id)

    def snapshot_hash(self) -> str:
        """The first 16 bytes of SHA-256 over the tracked fields."""
        return hashlib.sha256(self.value).digest()[:16].hex()

    def snapshot_value(self) -> bytes:
        """The tracked JSON, so a removal or update carries the last listed state."""
        return self.value

    def lists_before(self, other: "ListedMeeting") -> bool:
        """Order two listings of one meeting ID: the earlier start, then the smaller hash."""
        start, other_start = self.summary.start_time, other.summary.start_time
        if start is not None and other_start is not None:
            start_nanoseconds, other_nanoseconds = (
                _instant_nanoseconds(start),
                _instant_nanoseconds(other_start),
            )
            if start_nanoseconds != other_nanoseconds:
                return start_nanoseconds < other_nanoseconds
        elif (start is None) != (other_start is None):
            return start is not None
        return self.snapshot_hash() < other.snapshot_hash()


def decode_tracked_meeting(entry: KeyedSnapshotEntry) -> MeetingSummary:
    """Read a stored snapshot value back as a summary without UUID or join URL.

    Raises ValueError for a value Go's trackedMeeting decoding would refuse or whose ID differs from its key.
    """
    invalid = ValueError("stored Zoom meeting snapshot entry is invalid")
    try:
        tracked = json.loads(entry.value or b"")
    except (UnicodeDecodeError, ValueError) as error:
        raise invalid from error
    if not isinstance(tracked, dict):
        raise invalid
    meeting_id = tracked.get("id")
    if type(meeting_id) is not int or meeting_id < 1 or str(meeting_id) != entry.key:
        raise invalid

    def text(name: str) -> str:
        item = tracked.get(name)
        if item is None:
            return ""
        if not isinstance(item, str):
            raise invalid
        return item

    def integer(name: str) -> int:
        item = tracked.get(name)
        if item is None:
            return 0
        if type(item) is not int or not -(2**63) <= item < 2**63:
            raise invalid
        return item

    def timestamp(name: str) -> str | None:
        item = tracked.get(name)
        if item is None:
            return None
        if not isinstance(item, str):
            raise invalid
        return item

    try:
        return MeetingSummary(
            id=meeting_id,
            host_id=text("hostId"),
            topic=text("topic"),
            type=integer("type"),
            start_time=timestamp("startTime"),
            duration_minutes=integer("durationMinutes"),
            time_zone=text("timeZone"),
            agenda=text("agenda"),
            created_at=timestamp("createdAt"),
        )
    except ValueError as error:
        raise invalid from error


def meeting_change_id(
    kind: str, key: str, previous: KeyedSnapshotEntry | None, current_hash: str, sequence: int
) -> str:
    """Hash the change so a refetch keeps its ID; a change back and forth or a recovery gets a new one.

    An update or removal includes the previous entry's hash and commit; a scheduled change, which has no
    previous entry, includes the cursor's count of commits with changes.
    """
    parts = [kind, key, current_hash]
    if previous is not None:
        parts += [previous.hash, str(previous.seen)]
    elif sequence > 0:
        parts.append(str(sequence))
    joined = "\x00".join(_go_string(part) for part in parts)
    return "zoom-meeting-" + hashlib.sha256(joined.encode("utf-8")).digest()[:16].hex()


def new_meeting_changed_trigger_event(
    kind: str,
    key: str,
    previous: KeyedSnapshotEntry | None,
    current_hash: str,
    sequence: int,
    payload: MeetingChangedEvent,
    observed_at: datetime,
) -> TriggerEvent[MeetingChangedEvent]:
    """Build the Trigger event; a scheduled meeting occurs when created, any other change when observed."""
    occurred_at = observed_at.astimezone(UTC)
    if kind == MeetingChangeKind.SCHEDULED and payload.meeting.created_at is not None:
        occurred_at = _timestamp_datetime(payload.meeting.created_at)
    return TriggerEvent(meeting_change_id(kind, key, previous, current_hash, sequence), occurred_at, payload)
