# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoom meeting wire validation and conversion."""

import json
import re
from datetime import UTC, datetime, timedelta
from typing import TypeVar
from urllib.parse import urlsplit
from zoneinfo import ZoneInfo, ZoneInfoNotFoundError

from .models import (
    Meeting,
    MeetingPage,
    MeetingParticipant,
    MeetingSettings,
    MeetingSettingsInput,
    MeetingSummary,
    ParticipantPage,
)

T = TypeVar("T")


def _field(value: dict[str, object], name: str, kind: type[T], default: T) -> T:
    item = value.get(name, default)
    if item is None:
        return default
    if type(item) is not kind:
        raise ValueError("Zoom returned an unusable meeting")
    return item


def _object(value: object) -> dict[str, object]:
    if not isinstance(value, dict) or not all(isinstance(key, str) for key in value):
        raise ValueError("Zoom returned an unusable meeting")
    return value


def _list(value: dict[str, object], name: str) -> list[object]:
    items = value.get(name)
    if items is None:
        return []
    if not isinstance(items, list):
        raise ValueError("Zoom returned an unusable meeting")
    return items


_ZOOM_TIME = re.compile(
    r"^([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
    r"(?:[.,]([0-9]+))?(?:Z|([+-])([0-9]{2}):([0-9]{2}))$"
)


def _parse_zoom_time(value: str) -> tuple[datetime, str]:
    match = _ZOOM_TIME.fullmatch(value)
    if match is None:
        raise ValueError("Zoom returned an unusable time")
    year, month, day, hour, minute, second, fraction, sign, zone_hour, zone_minute = match.groups()
    try:
        instant = datetime(int(year), int(month), int(day), int(hour), int(minute), int(second), tzinfo=UTC)
    except ValueError as error:
        raise ValueError("Zoom returned an unusable time") from error
    offset = 0
    if sign is not None:
        if int(zone_minute) > 59:
            raise ValueError("Zoom returned an unusable time")
        offset = (int(zone_hour) * 60 + int(zone_minute)) * (1 if sign == "+" else -1)
    instant -= timedelta(minutes=offset)
    return instant, (fraction or "")[:9]


def _time(value: object) -> str | None:
    if not isinstance(value, str):
        raise ValueError("Zoom returned an unusable meeting")
    if not value:
        return None
    instant, fraction = _parse_zoom_time(value)
    fraction = fraction.rstrip("0")
    return instant.strftime("%Y-%m-%dT%H:%M:%S") + ("." + fraction if fraction else "") + "Z"


def _document(body: bytes) -> dict[str, object]:
    try:
        return _object(json.loads(body))
    except (UnicodeDecodeError, json.JSONDecodeError, TypeError) as error:
        raise ValueError("Zoom returned an unusable meeting") from error


def decode_meeting(value: dict[str, object]) -> Meeting:
    """Validate one meeting without retaining host start URLs or passcodes."""
    meeting_id = _field(value, "id", int, 0)
    duration = _field(value, "duration", int, 0)
    if meeting_id < 1 or duration < 0:
        raise ValueError("Zoom returned an unusable meeting")
    join_url = _field(value, "join_url", str, "")
    if join_url:
        parsed = urlsplit(join_url)
        if parsed.scheme != "https" or not parsed.hostname or parsed.username is not None:
            raise ValueError("Zoom returned an unusable meeting")
    settings_resource = value.get("settings")
    settings = _object(settings_resource) if settings_resource is not None else {}
    return Meeting(
        id=meeting_id,
        uuid=_field(value, "uuid", str, ""),
        host_id=_field(value, "host_id", str, ""),
        host_email=_field(value, "host_email", str, ""),
        topic=_field(value, "topic", str, ""),
        type=_field(value, "type", int, 0),
        status=_field(value, "status", str, ""),
        start_time=_time(_field(value, "start_time", str, "")),
        duration_minutes=duration,
        time_zone=_field(value, "timezone", str, ""),
        agenda=_field(value, "agenda", str, ""),
        join_url=join_url,
        created_at=_time(_field(value, "created_at", str, "")),
        settings=MeetingSettings(
            host_video=_field(settings, "host_video", bool, False),
            participant_video=_field(settings, "participant_video", bool, False),
            join_before_host=_field(settings, "join_before_host", bool, False),
            mute_upon_entry=_field(settings, "mute_upon_entry", bool, False),
            waiting_room=_field(settings, "waiting_room", bool, False),
            auto_recording=_field(settings, "auto_recording", str, ""),
        ),
    )


def decode_meeting_body(body: bytes) -> Meeting:
    """Decode one complete Zoom meeting response."""
    return decode_meeting(_document(body))


def decode_meeting_page(body: bytes) -> MeetingPage:
    """Decode one complete Zoom meeting page."""
    value = _document(body)
    total = _field(value, "total_records", int, 0)
    token = _field(value, "next_page_token", str, "")
    validate_page_token(token)
    if total < 0:
        raise ValueError("Zoom returned an unusable meeting page")
    meetings = []
    for item in _list(value, "meetings"):
        meeting = decode_meeting(_object(item))
        meetings.append(
            MeetingSummary(
                meeting.id,
                meeting.uuid,
                meeting.host_id,
                meeting.topic,
                meeting.type,
                meeting.start_time,
                meeting.duration_minutes,
                meeting.time_zone,
                meeting.agenda,
                meeting.join_url,
                meeting.created_at,
            )
        )
    return MeetingPage(tuple(meetings), token, total)


def decode_participant_page(body: bytes, meeting_id: int) -> ParticipantPage:
    """Decode one complete Zoom past-participant page."""
    value = _document(body)
    total = _field(value, "total_records", int, 0)
    token = _field(value, "next_page_token", str, "")
    validate_page_token(token)
    if total < 0:
        raise ValueError("Zoom returned an unusable participant page")
    participants = []
    for item in _list(value, "participants"):
        resource = _object(item)
        duration = _field(resource, "duration", int, 0)
        if duration < 0:
            raise ValueError("Zoom returned an unusable participant page")
        participants.append(
            MeetingParticipant(
                id=_field(resource, "id", str, ""),
                name=_field(resource, "name", str, ""),
                email=_field(resource, "user_email", str, ""),
                join_time=_time(_field(resource, "join_time", str, "")),
                leave_time=_time(_field(resource, "leave_time", str, "")),
                duration_seconds=duration,
                status=_field(resource, "status", str, ""),
                is_internal_user=_field(resource, "internal_user", bool, False),
            )
        )
    return ParticipantPage(meeting_id, tuple(participants), token, total)


def validate_page_token(token: str) -> None:
    """Require Zoom's printable ASCII continuation token format."""
    if len(token.encode()) > 1024 or any(not 33 <= ord(character) <= 126 for character in token):
        raise ValueError("pageToken must be printable ASCII without spaces and at most 1024 bytes")


def page_size(value: int) -> int:
    """Apply Zoom's documented page default and bounds."""
    if value == 0:
        return 30
    if type(value) is not int or not 1 <= value <= 300:
        raise ValueError("pageSize must be between 1 and 300")
    return value


def start_instant(value: str, zone: str) -> datetime:
    """Require a whole-second RFC 3339 instant and known IANA display zone."""
    try:
        instant, fraction = _parse_zoom_time(value)
    except ValueError as error:
        raise ValueError(f"startTime {value!r} must be RFC 3339 with an explicit offset") from error
    if any(digit != "0" for digit in fraction):
        raise ValueError(f"startTime {value!r} must be a whole second, because Zoom stores seconds")
    if not zone:
        raise ValueError("timeZone is required with startTime; use an IANA name such as America/Los_Angeles")
    if zone != zone.strip() or zone == "Local":
        raise ValueError(f"timeZone {zone!r} must be an IANA time zone name such as America/Los_Angeles")
    try:
        ZoneInfo(zone)
    except ZoneInfoNotFoundError as error:
        raise ValueError(f"timeZone {zone!r} is not a known IANA time zone name") from error
    return instant.astimezone(UTC)


def settings_payload(settings: MeetingSettingsInput | None) -> dict[str, object] | None:
    """Send only set meeting preferences to Zoom."""
    if settings is None:
        return None
    if isinstance(settings, dict):
        settings = MeetingSettingsInput(
            host_video=settings.get("hostVideo"),
            participant_video=settings.get("participantVideo"),
            join_before_host=settings.get("joinBeforeHost"),
            mute_upon_entry=settings.get("muteUponEntry"),
            waiting_room=settings.get("waitingRoom"),
            auto_recording=settings.get("autoRecording"),
        )
    if settings.auto_recording not in (None, "", "none", "local", "cloud"):
        raise ValueError("settings.autoRecording must be none, local, or cloud")
    payload: dict[str, object] = {}
    for name in ("host_video", "participant_video", "join_before_host", "mute_upon_entry", "waiting_room"):
        value = getattr(settings, name)
        if value is not None:
            payload[name] = value
    if settings.auto_recording:
        payload["auto_recording"] = settings.auto_recording
    return payload or None
