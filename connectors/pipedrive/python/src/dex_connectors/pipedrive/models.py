# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Generated Pipedrive models and strict API v2 response decoding."""

import json
import math
import re
from datetime import UTC, datetime, timedelta
from typing import Any

from ._generated.models import (
    CreateObjectInput as CreateObjectInput,
)
from ._generated.models import (
    CRMObject as CRMObject,
)
from ._generated.models import (
    GetObjectInput as GetObjectInput,
)
from ._generated.models import (
    ListObjectsInput as ListObjectsInput,
)
from ._generated.models import (
    ObjectPage as ObjectPage,
)
from ._generated.models import (
    SearchObjectsInput as SearchObjectsInput,
)
from ._generated.models import (
    UpdateObjectInput as UpdateObjectInput,
)
from ._generated.models import (
    UpsertedObject as UpsertedObject,
)
from ._generated.models import (
    UpsertObjectInput as UpsertObjectInput,
)

_ID = re.compile(r"[1-9][0-9]{0,18}\Z")
_CURSOR = re.compile(r"[A-Za-z0-9_\-=+/.]{1,1000}\Z")
_RFC3339 = re.compile(
    r"([0-9]{4})-([0-9]{2})-([0-9]{2})T([0-9]{2}):([0-9]{2}):([0-9]{2})"
    r"(?:[.,]([0-9]+))?(Z|([+-])([0-9]{2}):([0-9]{2}))\Z"
)


class PrecisionLossError(ValueError):
    """A provider JSON number cannot survive the Python output codec unchanged."""


def validate_object_type(object_type: str) -> None:
    """Require one Pipedrive API v2 record family."""
    if object_type not in ("persons", "organizations", "deals"):
        raise ValueError("object type must be persons, organizations, or deals")


def validate_record_id(name: str, value: str) -> None:
    """Require the decimal record ID accepted by the Go connector."""
    if not isinstance(value, str) or not _ID.fullmatch(value):
        raise ValueError(f"{name} must be a positive decimal Pipedrive ID")


def validate_limit(value: int) -> int:
    """Apply Pipedrive's 25-record default and 500-record ceiling."""
    if value == 0:
        return 25
    if type(value) is not int or not 1 <= value <= 500:
        raise ValueError("limit must be from 1 through 500")
    return value


def validate_cursor(value: str) -> None:
    """Check a provider cursor without changing its spelling."""
    if value and (not isinstance(value, str) or not _CURSOR.fullmatch(value)):
        raise ValueError("cursor must be the nextCursor of a previous page")


def _object(value: object) -> dict[str, Any]:
    if not isinstance(value, dict) or any(not isinstance(key, str) for key in value):
        raise ValueError("Pipedrive returned a malformed response")
    return value


def _id(value: object) -> str:
    if value is None:
        return ""
    if type(value) is not int or value > 9223372036854775807 or value <= 0:
        raise ValueError("Pipedrive returned a malformed response")
    text = str(value)
    if not _ID.fullmatch(text):
        raise ValueError("Pipedrive returned a malformed response")
    return text


def _string(value: object) -> str:
    return value if isinstance(value, str) else ""


def parse_go_rfc3339(value: str, *, is_go_json_time: bool = False) -> tuple[datetime, int]:
    """Parse provider RFC 3339, or Go JSON time with its wider offset bounds."""
    match = _RFC3339.fullmatch(value)
    if match is None:
        raise ValueError("invalid RFC 3339 timestamp")
    year, month, day, hour, minute, second, fraction, zone, sign, zone_hour, zone_minute = match.groups()
    if int(hour) > 23 or int(minute) > 59 or int(second) > 59:
        raise ValueError("invalid RFC 3339 clock")
    if zone != "Z" and (
        int(zone_hour) > (24 if is_go_json_time else 99)
        or int(zone_minute) > (60 if is_go_json_time else 59)
    ):
        raise ValueError("invalid RFC 3339 offset")
    nanoseconds = int((fraction or "")[:9].ljust(9, "0"))
    local = datetime(int(year), int(month), int(day), int(hour), int(minute), int(second), tzinfo=UTC)
    if zone != "Z":
        offset = timedelta(hours=int(zone_hour), minutes=int(zone_minute))
        local += -offset if sign == "+" else offset
    return local, nanoseconds


def _time(value: object) -> str:
    if isinstance(value, str):
        try:
            parsed, nanoseconds = parse_go_rfc3339(value)
            fraction = f"{nanoseconds:09d}".rstrip("0")
            return parsed.strftime("%Y-%m-%dT%H:%M:%S") + (f".{fraction}" if fraction else "") + "Z"
        except (ValueError, OverflowError):
            pass
    return "0001-01-01T00:00:00Z"


def decode_envelope(body: bytes) -> tuple[object, dict[str, Any]]:
    """Decode the success wrapper while rejecting missing or null data."""
    try:
        envelope = _object(json.loads(body, parse_float=_exact_fraction, parse_constant=_reject_fraction))
    except PrecisionLossError:
        raise
    except (ValueError, UnicodeDecodeError):
        raise ValueError("Pipedrive returned a malformed response") from None
    if (
        type(envelope.get("success")) is not bool
        or envelope["success"] is not True
        or envelope.get("data") is None
    ):
        raise ValueError("Pipedrive returned a malformed response")
    return envelope["data"], envelope


def _reject_fraction(value: str) -> object:
    raise PrecisionLossError(
        "Pipedrive returned a JSON number that fields or custom_fields cannot preserve exactly"
    )


def _exact_fraction(value: str) -> float:
    number = float(value)
    if not math.isfinite(number) or json.dumps(number) != value:
        _reject_fraction(value)
    return number


def _next_cursor(envelope: dict[str, Any]) -> str:
    additional = envelope.get("additional_data")
    if additional is None:
        return ""
    additional = _object(additional)
    cursor = additional.get("next_cursor")
    if cursor is None or cursor == "":
        return ""
    if not isinstance(cursor, str) or not _CURSOR.fullmatch(cursor):
        raise ValueError("Pipedrive returned an invalid next cursor")
    return cursor


def decode_record(object_type: str, value: object, *, partial: bool = False) -> CRMObject:
    """Preserve returned JSON fields and derive typed Pipedrive associations."""
    fields = _object(value).copy()
    try:
        identifier = _id(fields.get("id"))
    except ValueError:
        identifier = ""
    if not identifier:
        description = "search result" if partial else "record"
        raise ValueError(f"Pipedrive returned a {description} without a valid ID")
    custom_fields: dict[str, Any] = {}
    if not partial and "custom_fields" in fields:
        raw_custom = fields.pop("custom_fields")
        if raw_custom is not None:
            custom_fields = _object(raw_custom)
    name = _string(fields.get("title" if object_type == "deals" else "name"))
    values: dict[str, Any] = {
        "object_type": object_type,
        "id": identifier,
        "name": name,
        "fields": fields,
        "custom_fields": custom_fields,
        "is_partial": partial,
    }
    if partial:
        for source, destination in (
            ("owner", "owner_id"),
            ("organization", "organization_id"),
            ("person", "person_id"),
            ("stage", "stage_id"),
        ):
            linked = fields.get(source)
            if linked is not None:
                try:
                    values[destination] = _id(_object(linked).get("id"))
                except ValueError:
                    raise ValueError(f"Pipedrive returned an invalid {source} in a search result") from None
        if (
            object_type == "persons"
            and isinstance(fields.get("emails"), list)
            and all(isinstance(e, str) for e in fields["emails"])
        ):
            values["emails"] = tuple(fields["emails"])
    else:
        associations = ["owner_id"]
        if object_type != "organizations":
            associations.append("org_id")
        if object_type == "deals":
            associations.extend(("person_id", "pipeline_id", "stage_id"))
        for key in associations:
            destination = "organization_id" if key == "org_id" else key
            try:
                values[destination] = _id(fields.get(key))
            except ValueError:
                raise ValueError(f"Pipedrive returned an invalid {key}") from None
        if object_type == "persons":
            emails = fields.get("emails")
            if isinstance(emails, list) and all(
                isinstance(entry, dict)
                and isinstance(entry.get("value", ""), str)
                and isinstance(entry.get("primary", False), bool)
                for entry in emails
            ):
                primary = [entry["value"] for entry in emails if entry.get("value") and entry.get("primary")]
                others = [
                    entry["value"] for entry in emails if entry.get("value") and not entry.get("primary")
                ]
                values["emails"] = tuple(primary + others)
        values["add_time"] = _time(fields.get("add_time"))
        values["update_time"] = _time(fields.get("update_time"))
    if object_type == "deals" and _string(fields.get("status")):
        values["status"] = _string(fields.get("status"))
    return CRMObject(**values)


def decode_record_body(object_type: str, body: bytes) -> CRMObject:
    """Decode a get, create, or update result."""
    data, _ = decode_envelope(body)
    return decode_record(object_type, data)


def decode_page(object_type: str, body: bytes, *, search: bool) -> ObjectPage:
    """Decode a full list page or partial search page."""
    data, envelope = decode_envelope(body)
    if search:
        items = _object(data).get("items")
        if items is None:
            items = []
        if not isinstance(items, list):
            raise ValueError("Pipedrive returned a malformed response")
        records = [_object(item).get("item") for item in items]
    else:
        if not isinstance(data, list):
            raise ValueError("Pipedrive returned a malformed response")
        records = data
    objects = tuple(decode_record(object_type, record, partial=search) for record in records)
    return ObjectPage(object_type, objects, _next_cursor(envelope))
