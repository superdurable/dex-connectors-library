# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""The pageChanged cursor token and snapshot values, encoded and decoded byte for byte as Go does."""

import hashlib
import json
import re
from dataclasses import dataclass
from typing import Any, Final

from ._go_time import GoInstant, parse_go_time

_GO_HTML_ESCAPES: Final = (
    ("<", "\\u003c"),
    (">", "\\u003e"),
    ("&", "\\u0026"),
    (" ", "\\u2028"),
    (" ", "\\u2029"),
)
_LONE_SURROGATE: Final = re.compile("[\ud800-\udfff]")
_INT64_MINIMUM: Final = -(2**63)
_INT64_MAXIMUM: Final = 2**63 - 1


class _UndecodableError(ValueError):
    pass


@dataclass(slots=True)
class PageChangeScan:
    """A scan that spans several search pages; Confluence's search cursor continues only its own CQL."""

    started_at: GoInstant = GoInstant()
    cql: str = ""
    search_cursor: str = ""


@dataclass(slots=True)
class PageChangeCursor:
    """The committed position: when the last complete scan began, and any scan in progress."""

    cloud_id: str = ""
    space_key: str = ""
    scanned_through: GoInstant = GoInstant()
    scan: PageChangeScan | None = None


@dataclass(frozen=True, slots=True)
class SeenPageVersion:
    """The snapshot value of one delivered or baseline page version."""

    version_number: int = 0
    last_modified_at: GoInstant = GoInstant()


def go_json(value: object) -> str:
    """Encode a string, number, list, or dict as Go's encoding/json does, escaping HTML characters."""
    if isinstance(value, str):
        value = _LONE_SURROGATE.sub("�", value)
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"))
    for character, escaped in _GO_HTML_ESCAPES:
        encoded = encoded.replace(character, escaped)
    return encoded


def encode_page_change_cursor(position: PageChangeCursor) -> str:
    """Encode position as Go's json.Marshal does, with the same field order and omitted empty fields."""
    fields = [f'"cloudId":{go_json(position.cloud_id)}']
    if position.space_key:
        fields.append(f'"spaceKey":{go_json(position.space_key)}')
    fields.append(f'"scannedThrough":"{position.scanned_through.format()}"')
    if position.scan is not None:
        scan = [f'"startedAt":"{position.scan.started_at.format()}"', f'"cql":{go_json(position.scan.cql)}']
        if position.scan.search_cursor:
            scan.append(f'"searchCursor":{go_json(position.scan.search_cursor)}')
        fields.append('"scan":{' + ",".join(scan) + "}")
    return "{" + ",".join(fields) + "}"


def decode_page_change_cursor(token: str) -> PageChangeCursor:
    """Decode a stored token as Go's json.Unmarshal does; raises ValueError for one Go cannot decode.

    Keys match case-insensitively, unknown keys are ignored, and null leaves a field empty.
    """
    try:
        document = json.loads(token, parse_constant=_reject_constant)
    except (ValueError, RecursionError):
        raise ValueError("Confluence pageChanged cursor is not decodable") from None
    if document is None:
        return PageChangeCursor()
    try:
        values = _object_fields(document, ("cloudid", "spacekey", "scannedthrough", "scan"))
        position = PageChangeCursor(
            cloud_id=_string(values.get("cloudid")),
            space_key=_string(values.get("spacekey")),
            scanned_through=_time(values.get("scannedthrough")),
        )
        scan = values.get("scan")
        if scan is not None:
            scan_values = _object_fields(scan, ("startedat", "cql", "searchcursor"))
            position.scan = PageChangeScan(
                _time(scan_values.get("startedat")),
                _string(scan_values.get("cql")),
                _string(scan_values.get("searchcursor")),
            )
    except _UndecodableError:
        raise ValueError("Confluence pageChanged cursor is not decodable") from None
    return position


def encode_seen_page_version(seen: SeenPageVersion) -> bytes:
    """Encode the snapshot value as Go marshals seenPageVersion."""
    return (
        f'{{"versionNumber":{seen.version_number},"lastModifiedAt":"{seen.last_modified_at.format()}"}}'
    ).encode()


def decode_seen_page_version(value: bytes | None) -> SeenPageVersion:
    """Decode a snapshot value as Go's json.Unmarshal does; raises ValueError for one Go cannot decode."""
    try:
        document = json.loads(value or b"", parse_constant=_reject_constant)
        if document is None:
            return SeenPageVersion()
        values = _object_fields(document, ("versionnumber", "lastmodifiedat"))
        return SeenPageVersion(_integer(values.get("versionnumber")), _time(values.get("lastmodifiedat")))
    except (ValueError, RecursionError, _UndecodableError):
        raise ValueError("Confluence seen page version is not decodable") from None


def seen_page_hash(page_id: str, version_number: int) -> str:
    """Hash the page version as Go's seenPageItem.SnapshotHash does."""
    return hashlib.sha256(f"{_go_string(page_id)}\x00{version_number}".encode()).digest()[:8].hex()


def page_change_event_id(cloud_id: str, page_id: str, version_number: int) -> str:
    """Return the pageChanged Trigger event ID: a hash of site, page ID, and version, as Go computes it."""
    joined = "\x00".join((_go_string(cloud_id), _go_string(page_id), str(version_number)))
    return "confluence-page-" + hashlib.sha256(joined.encode()).digest()[:16].hex()


def _go_string(value: str) -> str:
    return _LONE_SURROGATE.sub("�", value)


def _object_fields(document: object, names: tuple[str, ...]) -> dict[str, Any]:
    """Return the known fields of a JSON object by folded name; the last spelling wins, as in Go."""
    if not isinstance(document, dict):
        raise _UndecodableError
    values: dict[str, Any] = {}
    for key, value in document.items():
        folded = key.lower() if key.isascii() else key
        if folded in names:
            values[folded] = value
    return values


def _string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise _UndecodableError
    return value


def _integer(value: object) -> int:
    if value is None:
        return 0
    if type(value) is not int or not _INT64_MINIMUM <= value <= _INT64_MAXIMUM:
        raise _UndecodableError
    return value


def _time(value: object) -> GoInstant:
    if value is None:
        return GoInstant()
    if not isinstance(value, str):
        raise _UndecodableError
    try:
        return parse_go_time(value)
    except ValueError:
        raise _UndecodableError from None


def _reject_constant(value: str) -> Any:
    raise ValueError(f"{value} is not JSON")
