# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The issueChanged cursor token, encoded and decoded byte for byte as the Go connector does."""

import json
import re
from dataclasses import dataclass, field
from typing import Any, Final

from dex_connectors.sdk.pollingtrigger import Cursor

ISSUE_CHANGE_LOOKBACK_MILLISECONDS: Final = 120_000
"""Re-read window before the cursor: JQL dates have minute precision and Jira indexes updates late."""
MAXIMUM_SEEN_ISSUE_VERSIONS: Final = 1000
"""Keeps the cursor far below the poll kit's 64 KiB token bound."""

_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1
_GO_INTEGER: Final = re.compile(r"[+-]?[0-9]+\Z", re.ASCII)
# Go's encoding/json escapes these even inside strings, so stored tokens match a Go replica's bytes.
_GO_HTML_ESCAPES: Final = {"<": "\\u003c", ">": "\\u003e", "&": "\\u0026", " ": "\\u2028", " ": "\\u2029"}
_STRING_FIELDS: Final = {"cloudid": "cloud_id", "project": "project", "pagetoken": "page_token"}
_INTEGER_FIELDS: Final = {
    "updatedthrough": "updated_through",
    "searchminutes": "search_minutes",
    "searchstartedat": "search_started_at",
    "searchfloor": "search_floor",
}


@dataclass(slots=True)
class IssueChangeCursor:
    """The committed position: the latest updated instant read and the versions seen near it.

    updated_through is in Unix milliseconds; seen_versions lists "issueID@updatedMillis" versions within
    the lookback before it. page_token and the search fields are set only while one search spans pages,
    because Jira requires the next page to repeat the same JQL; search_floor is the restart floor, in Unix
    milliseconds, the search began from.
    """

    cloud_id: str = ""
    project: str = ""
    updated_through: int = 0
    seen_versions: list[str] = field(default_factory=list)
    page_token: str = ""
    search_minutes: int = 0
    search_started_at: int = 0
    search_floor: int = 0


def encode_issue_change_cursor(position: IssueChangeCursor) -> Cursor:
    """Encode position as Go's json.Marshal does, with the same field order and omitted empty fields."""
    document: dict[str, Any] = {"cloudId": position.cloud_id}
    if position.project:
        document["project"] = position.project
    document["updatedThrough"] = position.updated_through
    if position.seen_versions:
        document["seenVersions"] = position.seen_versions
    for name, value in (
        ("pageToken", position.page_token),
        ("searchMinutes", position.search_minutes),
        ("searchStartedAt", position.search_started_at),
        ("searchFloor", position.search_floor),
    ):
        if value:
            document[name] = value
    encoded = json.dumps(document, ensure_ascii=False, separators=(",", ":"))
    for character, escaped in _GO_HTML_ESCAPES.items():
        encoded = encoded.replace(character, escaped)
    return Cursor(encoded)


def decode_issue_change_cursor(cursor: Cursor) -> IssueChangeCursor:
    """Decode a stored token as Go's json.Unmarshal does and require a complete position.

    Keys match case-insensitively, unknown keys are ignored, null leaves a field empty, and a number that
    is not an int64 literal fails. Raises ValueError for anything undecodable.
    """
    try:
        document = json.loads(cursor.token, parse_float=_reject_number, parse_constant=_reject_number)
    except (ValueError, RecursionError):
        raise ValueError("Jira issue cursor is not decodable") from None
    if not isinstance(document, dict):
        raise ValueError("Jira issue cursor is not decodable")
    position = IssueChangeCursor()
    for key, value in document.items():
        folded = key.lower() if key.isascii() else key
        if folded in _STRING_FIELDS:
            if value is not None and not isinstance(value, str):
                raise ValueError("Jira issue cursor is not decodable")
            setattr(position, _STRING_FIELDS[folded], value or "")
        elif folded in _INTEGER_FIELDS:
            if value is not None and not _is_int64(value):
                raise ValueError("Jira issue cursor is not decodable")
            setattr(position, _INTEGER_FIELDS[folded], value or 0)
        elif folded == "seenversions":
            if value is not None and (
                not isinstance(value, list)
                or any(item is not None and not isinstance(item, str) for item in value)
            ):
                raise ValueError("Jira issue cursor is not decodable")
            position.seen_versions = [item or "" for item in value or []]
    if not position.cloud_id or position.updated_through <= 0:
        raise ValueError("Jira issue cursor is not decodable")
    if position.page_token and (
        position.search_minutes < 1 or position.search_started_at <= 0 or position.search_floor <= 0
    ):
        raise ValueError("Jira issue cursor has a page token without its search")
    return position


def recent_seen_versions(
    previous: list[str], versions: list[tuple[str, int]], updated_through: int
) -> list[str]:
    """Merge (issueID, updatedMillis) versions into previous and keep those within the lookback.

    Keeps only the newest when they exceed the bound; a dropped entry can only cause a redelivery with the
    same event ID. Malformed previous entries are dropped.
    """
    threshold = updated_through - ISSUE_CHANGE_LOOKBACK_MILLISECONDS
    by_key: dict[str, int] = {}
    for entry in previous:
        separator = entry.rfind("@")
        if separator < 1:
            continue
        millis_text = entry[separator + 1 :]
        if _GO_INTEGER.match(millis_text) and _INT64_MINIMUM <= int(millis_text) <= _INT64_MAXIMUM:
            by_key[entry] = int(millis_text)
    for issue_id, updated_millis in versions:
        by_key[issue_version_key(issue_id, updated_millis)] = updated_millis
    kept = [key for key, millis in by_key.items() if millis >= threshold]
    kept.sort(key=lambda key: (-by_key[key], key.encode()))
    return sorted(kept[:MAXIMUM_SEEN_ISSUE_VERSIONS], key=str.encode)


def issue_version_key(issue_id: str, updated_millis: int) -> str:
    """Return the "issueID@updatedMillis" key of one issue version."""
    return f"{issue_id}@{updated_millis}"


def _is_int64(value: object) -> bool:
    return (
        isinstance(value, int) and not isinstance(value, bool) and _INT64_MINIMUM <= value <= _INT64_MAXIMUM
    )


def _reject_number(value: str) -> Any:
    raise ValueError(f"{value} is not an int64")
