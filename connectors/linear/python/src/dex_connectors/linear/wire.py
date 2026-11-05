# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate Linear inputs and decode typed GraphQL records."""

from __future__ import annotations

import hashlib
import re
from dataclasses import fields
from datetime import datetime, timedelta
from typing import Any

from .models import (
    CycleReference,
    Issue,
    IssueLabel,
    IssueReference,
    IssueSummary,
    ProjectReference,
    TeamReference,
    UserReference,
    WorkflowStateReference,
)

_UUID = re.compile(r"[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}\Z")
_ISSUE = re.compile(r"([A-Za-z][A-Za-z0-9]{0,9})-([1-9][0-9]{0,8})\Z")
_TEAM_KEY = re.compile(r"[A-Za-z][A-Za-z0-9]{0,9}\Z")
_TIME = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})\Z")


def uuid_field(value: str, field: str, required: bool = False) -> str:
    """Validate one Linear UUID and canonicalize its hex case."""
    value = value.strip()
    if not value:
        if required:
            raise ValueError(f"{field} is required")
        return ""
    if not _UUID.fullmatch(value):
        raise ValueError(f"{field} must be a Linear UUID such as 2f6b7c1e-3d4a-4b5c-8d6e-7f8091a2b3c4")
    return value.lower()


def uuid_list(values: tuple[str, ...], field: str) -> list[str]:
    """Validate up to fifty distinct Linear IDs."""
    if len(values) > 50:
        raise ValueError(f"{field} accepts at most 50 IDs")
    selected: list[str] = []
    for value in values:
        identity = uuid_field(value, field, True)
        if identity in selected:
            raise ValueError(f"{field} lists {identity} twice")
        selected.append(identity)
    return selected


def issue_locator(value: str, field: str) -> tuple[str, dict[str, Any]]:
    """Return a mutation identifier and a matching IssueFilter."""
    value = value.strip()
    if not value:
        raise ValueError(f"{field} is required")
    if _UUID.fullmatch(value):
        canonical = value.lower()
        return canonical, {"id": {"eq": canonical}}
    match = _ISSUE.fullmatch(value)
    if match is None:
        raise ValueError(f"{field} must be an issue UUID or an identifier such as ENG-123")
    team, number = match.groups()
    return team.upper() + "-" + number, {"team": {"key": {"eq": team.upper()}}, "number": {"eq": int(number)}}


def one_line(value: str, field: str, maximum: int = 255) -> str:
    """Validate and trim a title or label name."""
    value = value.strip()
    if not value:
        raise ValueError(f"{field} is required")
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        raise ValueError(f"{field} must be valid UTF-8") from None
    if len(value) > maximum:
        raise ValueError(f"{field} cannot exceed {maximum} characters")
    if any(ord(character) < 32 or ord(character) == 127 for character in value):
        raise ValueError(f"{field} must be one line without control characters")
    return value


def markdown(value: str, field: str, required: bool = False) -> str:
    """Validate bounded Markdown with line breaks and tabs."""
    if required and not value.strip():
        raise ValueError(f"{field} is required")
    try:
        value.encode("utf-8")
    except UnicodeEncodeError:
        raise ValueError(f"{field} must be valid UTF-8") from None
    if len(value) > 65536:
        raise ValueError(f"{field} cannot exceed 65536 characters")
    if any(
        (ord(character) < 32 and character not in "\r\n\t") or ord(character) == 127 for character in value
    ):
        raise ValueError(f"{field} cannot contain control characters")
    return value


def priority(value: int | None) -> None:
    """Validate Linear's five priority values."""
    if value is not None and (type(value) is not int or value < 0 or value > 4):
        raise ValueError("priority must be 0 (none), 1 (urgent), 2 (high), 3 (medium), or 4 (low)")


def due_date(value: str) -> str:
    """Validate a calendar date with Go time.Parse semantics."""
    value = value.strip()
    if not value:
        return ""
    try:
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
            raise ValueError
        datetime.strptime(value, "%Y-%m-%d")
    except ValueError:
        raise ValueError("dueDate must be a YYYY-MM-DD date such as 2026-02-18") from None
    return value


def email(value: str) -> str:
    """Match Linear Go's single-address shape check."""
    value = value.strip()
    local, separator, domain = value.partition("@")
    if not value:
        raise ValueError("email is required")
    if (
        len(value.encode()) > 320
        or not separator
        or not local
        or not domain
        or "@" in domain
        or "." not in domain
    ):
        raise ValueError("email must be one address such as alice@example.com")
    if any(ord(character) <= 32 or ord(character) == 127 for character in value):
        raise ValueError("email cannot contain spaces or control characters")
    return value


def client_entity_id(key: str, entity: str) -> str:
    """Derive Go's stable client supplied UUID from a Dex Call ID."""
    if not key.strip():
        raise ValueError("Linear write has no idempotency key")
    digest = bytearray(hashlib.sha256(f"dex-linear-client-id/v1\x00{entity}\x00{key}".encode()).digest()[:16])
    digest[6] = (digest[6] & 0x0F) | 0x40
    digest[8] = (digest[8] & 0x3F) | 0x80
    encoded = digest.hex()
    return f"{encoded[:8]}-{encoded[8:12]}-{encoded[12:16]}-{encoded[16:20]}-{encoded[20:]}"


def linear_time(value: Any) -> str:
    """Parse Linear RFC 3339 with Go's wide offset and nanosecond behavior."""
    if type(value) is not str:
        raise ValueError("Linear returned an invalid timestamp")
    match = _TIME.fullmatch(value)
    if match is None:
        raise ValueError("Linear returned an invalid timestamp")
    year, month, day, hour, minute, second, fraction, zone = match.groups()
    try:
        instant = datetime(int(year), int(month), int(day), int(hour), int(minute), int(second))
        if zone != "Z":
            sign = 1 if zone[0] == "+" else -1
            zone_hours, zone_minutes = map(int, zone[1:].split(":"))
            if zone_minutes > 59:
                raise ValueError
            instant -= timedelta(minutes=sign * (zone_hours * 60 + zone_minutes))
    except (ValueError, OverflowError):
        raise ValueError("Linear returned an invalid timestamp") from None
    nanos = (fraction or "")[:9].ljust(9, "0").rstrip("0")
    return instant.strftime("%Y-%m-%dT%H:%M:%S") + (f".{nanos}" if nanos else "") + "Z"


def _string(value: Any) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("Linear returned a malformed issue")
    return value


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError("Linear returned a malformed issue")
    return value


def _optional_string(value: Any) -> str:
    return "" if value is None else _string(value)


def issue_summary(value: Any) -> IssueSummary:
    """Decode and validate the common issue selection."""
    wire = _object(value)
    team = _object(wire.get("team"))
    state = _object(wire.get("state"))
    identity = _string(wire.get("id", ""))
    team_id = _string(team.get("id", ""))
    state_id = _string(state.get("id", ""))
    identifier = _string(wire.get("identifier", ""))
    raw_priority = wire.get("priority", 0)
    if raw_priority is None:
        raw_priority = 0
    if not (
        _UUID.fullmatch(identity)
        and identifier.strip()
        and _UUID.fullmatch(team_id)
        and _UUID.fullmatch(state_id)
    ):
        raise ValueError("Linear returned a malformed issue")
    if type(raw_priority) not in (int, float) or not 0 <= raw_priority <= 4:
        raise ValueError("Linear returned a malformed issue")
    labels = wire.get("labelIds")
    if labels is None:
        labels = []
    if not isinstance(labels, list) or not all(isinstance(label, str) for label in labels):
        raise ValueError("Linear returned a malformed issue")
    assignee = wire.get("assignee")
    assignee_id = _string(_object(assignee).get("id", "")) if assignee is not None else ""
    archived = wire.get("archivedAt")
    return IssueSummary(
        id=identity,
        identifier=identifier,
        title=_string(wire.get("title", "")),
        url=_string(wire.get("url", "")),
        team=TeamReference(id=team_id, key=_string(team.get("key", "")), name=_string(team.get("name", ""))),
        state=WorkflowStateReference(
            id=state_id, name=_string(state.get("name", "")), type=_string(state.get("type", ""))
        ),
        assignee_id=assignee_id,
        priority=int(raw_priority),
        label_ids=tuple(labels),
        due_date=_optional_string(wire.get("dueDate")),
        created_at=linear_time(wire.get("createdAt", "")),
        updated_at=linear_time(wire.get("updatedAt", "")),
        archived_at=linear_time(archived) if archived is not None else None,
    )


def issue(value: Any) -> Issue:
    """Decode full issue details and relations."""
    wire = _object(value)
    summary = issue_summary(wire)
    number = wire.get("number", 0)
    if number is None:
        number = 0
    if type(number) not in (int, float) or not 0 <= number <= 2147483647:
        raise ValueError("Linear returned a malformed issue")
    description = _optional_string(wire.get("description"))
    estimate = wire.get("estimate")
    trashed = wire.get("trashed")
    if estimate is not None and type(estimate) not in (int, float):
        raise ValueError("Linear returned a malformed issue")
    if trashed is not None and type(trashed) is not bool:
        raise ValueError("Linear returned a malformed issue")
    values = {field.name: getattr(summary, field.name) for field in fields(IssueSummary)}
    values.update(
        number=int(number),
        priority_label=_string(wire.get("priorityLabel", "")),
        estimate=float(estimate) if estimate is not None else None,
        description=description[:32768],
        description_truncated=len(description) > 32768,
        branch_name=_string(wire.get("branchName", "")),
        trashed=trashed is True,
        assignee=_user_reference(wire.get("assignee")),
        creator=_user_reference(wire.get("creator")),
        labels=_labels(wire.get("labels")),
        project=_project(wire.get("project")),
        cycle=_cycle(wire.get("cycle")),
        parent=_parent(wire.get("parent")),
        started_at=linear_time(wire["startedAt"]) if wire.get("startedAt") is not None else None,
        completed_at=linear_time(wire["completedAt"]) if wire.get("completedAt") is not None else None,
        canceled_at=linear_time(wire["canceledAt"]) if wire.get("canceledAt") is not None else None,
    )
    return Issue(**values)


def _user_reference(value: Any) -> UserReference | None:
    if value is None:
        return None
    data = _object(value)
    return UserReference(
        id=_string(data.get("id", "")),
        name=_string(data.get("name", "")),
        display_name=_string(data.get("displayName", "")),
    )


def _labels(value: Any) -> tuple[IssueLabel, ...]:
    if value is None:
        return ()
    nodes = _object(value).get("nodes")
    if nodes is None:
        return ()
    if not isinstance(nodes, list):
        raise ValueError("Linear returned a malformed issue")
    return tuple(
        IssueLabel(
            id=_string(_object(label).get("id", "")),
            name=_string(_object(label).get("name", "")),
            color=_string(_object(label).get("color", "")),
        )
        for label in nodes
    )


def _project(value: Any) -> ProjectReference | None:
    if value is None:
        return None
    data = _object(value)
    return ProjectReference(id=_string(data.get("id", "")), name=_string(data.get("name", "")))


def _cycle(value: Any) -> CycleReference | None:
    if value is None:
        return None
    data = _object(value)
    number = data.get("number", 0)
    if number is None:
        number = 0
    if type(number) not in (int, float):
        raise ValueError("Linear returned a malformed issue")
    return CycleReference(
        id=_string(data.get("id", "")), number=int(number), name=_optional_string(data.get("name"))
    )


def _parent(value: Any) -> IssueReference | None:
    if value is None:
        return None
    data = _object(value)
    return IssueReference(id=_string(data.get("id", "")), identifier=_string(data.get("identifier", "")))
