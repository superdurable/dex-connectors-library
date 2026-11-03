# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate Asana task fields and decode bounded provider task records."""

import math
import re
from datetime import UTC, date, datetime
from typing import Any

from .models import (
    CustomFieldValue,
    CustomFieldValueInput,
    EnumOptionReference,
    ProjectReference,
    SectionReference,
    Task,
    TaskMembership,
    TaskReference,
    UserReference,
    WorkspaceReference,
)

TASK_SUMMARY_FIELDS = ",".join(
    (
        "name",
        "resource_subtype",
        "completed",
        "completed_at",
        "assignee.name",
        "due_on",
        "due_at",
        "start_on",
        "memberships.project.name",
        "memberships.section.name",
        "parent.name",
        "permalink_url",
        "created_at",
        "modified_at",
    )
)
TASK_DETAIL_FIELDS = (
    TASK_SUMMARY_FIELDS
    + ","
    + ",".join(
        (
            "workspace.name",
            "notes",
            "custom_fields.name",
            "custom_fields.resource_subtype",
            "custom_fields.display_value",
            "custom_fields.text_value",
            "custom_fields.number_value",
            "custom_fields.enum_value.name",
            "custom_fields.multi_enum_values.name",
        )
    )
)
CREATED_TASK_FIELDS = "gid,name,permalink_url,created_at"
CREATED_STORY_FIELDS = "gid,created_at,created_by.gid,created_by.name,resource_subtype"
_GID = re.compile(r"^[0-9]{1,64}$")


def gid(value: str, field: str) -> str:
    """Require one decimal Asana identifier before using it in a provider request."""
    trimmed = value.strip()
    if not _GID.fullmatch(trimmed):
        raise ValueError(f"{field} must be an Asana gid, a decimal string such as 1204567890123456")
    return trimmed


def optional_gid(value: str, field: str) -> str:
    """Validate a present identifier while preserving an absent selector."""
    return gid(value, field) if value.strip() else ""


def due_date(value: str, field: str) -> str:
    """Require a real Asana calendar date in YYYY-MM-DD form."""
    trimmed = value.strip()
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", trimmed):
        raise ValueError(f"{field} must be a YYYY-MM-DD date such as 2026-10-15")
    try:
        date.fromisoformat(trimmed)
    except ValueError as error:
        raise ValueError(f"{field} must be a YYYY-MM-DD date such as 2026-10-15") from error
    return trimmed


def assignee(value: str, field: str) -> str:
    """Accept a user GID, `me`, or one workspace member email address."""
    trimmed = value.strip()
    if trimmed == "me" or _GID.fullmatch(trimmed):
        return trimmed
    if len(trimmed.encode()) <= 254 and re.fullmatch(r"[^\s@]+@[^\s@]+\.[^\s@]+", trimmed):
        return trimmed
    raise ValueError(f"{field} must be me, a user gid, or an email address")


def plain_text(value: str, field: str, required: bool) -> str:
    """Reject control characters and oversized plain text before transmission."""
    if required and not value.strip():
        raise ValueError(f"{field} is required")
    if len(value) > 65536:
        raise ValueError(f"{field} cannot exceed 65536 characters")
    if any(ord(character) < 32 and character not in "\t\n\r" or ord(character) == 127 for character in value):
        raise ValueError(f"{field} cannot contain control characters")
    return value


def task_name(value: str) -> str:
    """Require one trimmed, bounded task name without control characters."""
    name = value.strip()
    if not name:
        raise ValueError("name is required")
    if len(name) > 1024:
        raise ValueError("name cannot exceed 1024 characters")
    if any(ord(character) < 32 or ord(character) == 127 for character in name):
        raise ValueError("name must be one line without control characters")
    return name


def custom_field_values(inputs: tuple[CustomFieldValueInput, ...]) -> dict[str, Any] | None:
    """Encode supported custom field replacements with explicit zero values."""
    if not inputs:
        return None
    if len(inputs) > 20:
        raise ValueError("customFields accepts at most 20 values")
    values: dict[str, Any] = {}
    for item in inputs:
        field_id = gid(item.custom_field_id, "customFieldId")
        if field_id in values:
            raise ValueError(f"custom field {field_id} is set twice")
        selectors = (
            item.text is not None,
            item.number is not None,
            bool(item.enum_option_id.strip()),
            bool(item.multi_enum_option_ids),
        )
        if sum(selectors) != 1:
            raise ValueError(
                f"custom field {field_id}: set exactly one of text, number, "
                "enumOptionId, and multiEnumOptionIds"
            )
        if item.text is not None:
            values[field_id] = plain_text(item.text, "text", False)
        elif item.number is not None:
            if not math.isfinite(item.number):
                raise ValueError(f"custom field {field_id}: number must be finite")
            values[field_id] = item.number
        elif item.multi_enum_option_ids:
            if len(item.multi_enum_option_ids) > 50:
                raise ValueError(f"custom field {field_id}: multiEnumOptionIds accepts at most 50 options")
            values[field_id] = [gid(option, "multiEnumOptionIds") for option in item.multi_enum_option_ids]
        else:
            values[field_id] = gid(item.enum_option_id, "enumOptionId")
    return values


def parse_time(value: object) -> datetime:
    """Decode a required Asana RFC 3339 timestamp."""
    if not isinstance(value, str) or not value:
        raise ValueError("timestamp is not an ISO 8601 date-time")
    try:
        parsed = datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError("timestamp is not an ISO 8601 date-time") from error
    if parsed.tzinfo is None:
        raise ValueError("timestamp is not an ISO 8601 date-time")
    return parsed.astimezone(UTC)


def optional_time(value: object) -> datetime | None:
    """Decode an optional timestamp when Asana supplied one."""
    return parse_time(value) if value else None


def _record(value: object) -> dict[str, Any]:
    return value if isinstance(value, dict) else {}


def _string(value: object) -> str:
    return value if isinstance(value, str) else ""


def _named(value: object, reference_type: type[Any]) -> Any | None:
    resource = _record(value)
    identifier = _string(resource.get("gid"))
    if not _GID.fullmatch(identifier):
        return None
    return reference_type(identifier, _string(resource.get("name")))


def decode_task(value: object, detail: bool) -> Task:
    """Decode one task while bounding list sizes and excluding provider secrets."""
    resource = _record(value)
    identifier = gid(_string(resource.get("gid")), "task gid")
    memberships: list[TaskMembership] = []
    for raw in resource.get("memberships") or []:
        member = _record(raw)
        project = _named(member.get("project"), ProjectReference)
        if project is None or len(memberships) == 20:
            continue
        memberships.append(TaskMembership(project, _named(member.get("section"), SectionReference)))
    fields: list[CustomFieldValue] = []
    if detail:
        for raw in resource.get("custom_fields") or []:
            field = _record(raw)
            field_id = _string(field.get("gid"))
            if not _GID.fullmatch(field_id) or len(fields) == 50:
                continue
            options = tuple(
                option
                for raw_option in field.get("multi_enum_values") or []
                if (option := _named(raw_option, EnumOptionReference)) is not None
            )
            fields.append(
                CustomFieldValue(
                    id=field_id,
                    name=_string(field.get("name")),
                    type=_string(field.get("resource_subtype")),
                    display_value=_string(field.get("display_value")),
                    text_value=field.get("text_value") if isinstance(field.get("text_value"), str) else None,
                    number_value=field.get("number_value")
                    if isinstance(field.get("number_value"), (int, float))
                    else None,
                    enum_option=_named(field.get("enum_value"), EnumOptionReference),
                    multi_enum_options=options,
                )
            )
    notes = _string(resource.get("notes")) if detail else ""
    due_on = _string(resource.get("due_on"))
    start_on = _string(resource.get("start_on"))
    if due_on:
        due_on = due_date(due_on, "date")
    if start_on:
        start_on = due_date(start_on, "date")
    return Task(
        id=identifier,
        name=_string(resource.get("name")),
        resource_subtype=_string(resource.get("resource_subtype")),
        is_completed=resource.get("completed") is True,
        completed_at=optional_time(resource.get("completed_at")),
        assignee=_named(resource.get("assignee"), UserReference),
        due_on=due_on,
        due_at=optional_time(resource.get("due_at")),
        start_on=start_on,
        memberships=tuple(memberships),
        parent=_named(resource.get("parent"), TaskReference),
        workspace=_named(resource.get("workspace"), WorkspaceReference) if detail else None,
        notes=notes[:65536],
        is_notes_truncated=len(notes) > 65536,
        custom_fields=tuple(fields),
        permalink_url=_string(resource.get("permalink_url")),
        created_at=parse_time(resource.get("created_at")),
        modified_at=parse_time(resource.get("modified_at")),
    )
