# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate typed Notion inputs and encode provider request bodies."""

import math
import re
from datetime import datetime
from email.utils import parseaddr
from typing import cast
from urllib.parse import urlsplit

from .models import DateRange, PropertyValue, QueryFilter, QuerySort
from .provider_models import parse_id

_DATE = re.compile(r"^[0-9]{4}-[0-9]{2}-[0-9]{2}$")
_RFC3339 = re.compile(
    r"^[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(\.[0-9]+)?(Z|[+-][0-9]{2}:[0-9]{2})$"
)
_ZONE = re.compile(r"^[A-Za-z][A-Za-z0-9_+-]*(/[A-Za-z0-9_+-]+){0,3}$")
_SEPARATOR = re.compile(r"\n[ \t]*\n\s*")


def cursor(value: str) -> str:
    """Validate one opaque Notion cursor without changing its content."""
    result = value.strip()
    if result and (len(result.encode()) > 1024 or any(not " " < char <= "~" for char in result)):
        raise ValueError("a cursor must be the printable value from a previous page")
    return result


def select_data_source(data_source_id: str, database_id: str) -> tuple[str, str]:
    """Return exactly one canonical data source or database ID."""
    if bool(data_source_id.strip()) == bool(database_id.strip()):
        raise ValueError("set exactly one of dataSourceId and databaseId")
    if data_source_id.strip():
        return parse_id(data_source_id), ""
    return "", parse_id(database_id)


def _rich_text(text: str, maximum: int) -> list[dict[str, object]]:
    if len(text) > maximum:
        raise ValueError(f"text is at most {maximum} characters")
    chunks: list[str] = []
    chunk = ""
    units = 0
    for char in text:
        width = 2 if ord(char) > 0xFFFF else 1
        if units + width > 2000:
            chunks.append(chunk)
            chunk, units = "", 0
        chunk += char
        units += width
    if chunk:
        chunks.append(chunk)
    if len(chunks) > 100:
        raise ValueError("text needs more than 100 rich text objects")
    return [{"type": "text", "text": {"content": item}} for item in chunks]


def _date(value: DateRange) -> dict[str, object]:
    result: dict[str, object] = {"start": _date_text(value.start)}
    if value.end.strip():
        result["end"] = _date_text(value.end)
    zone = value.time_zone.strip()
    if zone:
        if len(zone) > 64 or not _ZONE.fullmatch(zone):
            raise ValueError("date timeZone must be an IANA time zone name")
        result["time_zone"] = zone
    return result


def _date_text(value: str) -> str:
    text = value.strip()
    try:
        if _DATE.fullmatch(text):
            datetime.strptime(text, "%Y-%m-%d")
        else:
            if not _RFC3339.fullmatch(text):
                raise ValueError
            parsed = datetime.fromisoformat(text.replace("Z", "+00:00"))
            if parsed.tzinfo is None or "T" not in text:
                raise ValueError
    except ValueError as error:
        raise ValueError("date must be YYYY-MM-DD or RFC 3339") from error
    return text


def _option(value: str) -> str:
    name = value.strip()
    if not name or len(name) > 100 or "," in name or "\n" in name or "\r" in name:
        raise ValueError("invalid Notion option name")
    return name


def _ids(values: tuple[str, ...], field: str) -> list[str]:
    if len(values) > 100:
        raise ValueError(f"{field} holds at most 100 IDs")
    result: list[str] = []
    for value in values:
        identifier = parse_id(value)
        if identifier not in result:
            result.append(identifier)
    return result


def encode_property(value: PropertyValue) -> dict[str, object]:
    """Encode one writable typed property with Go's clear semantics."""
    if isinstance(value, dict):
        date_value = value.get("date")
        if isinstance(date_value, dict):
            value = {
                **value,
                "date": DateRange(
                    start=date_value.get("start", ""),
                    end=date_value.get("end", ""),
                    time_zone=date_value.get("timeZone", date_value.get("time_zone", "")),
                ),
            }
        value = PropertyValue(
            **{
                (
                    "phone_number"
                    if key == "phoneNumber"
                    else "user_ids"
                    if key == "userIds"
                    else "page_ids"
                    if key == "pageIds"
                    else key
                ): setting
                for key, setting in value.items()
            }
        )
    field_for_type = {
        "title": "text",
        "rich_text": "text",
        "number": "number",
        "checkbox": "checkbox",
        "select": "option",
        "status": "option",
        "multi_select": "options",
        "date": "date",
        "url": "url",
        "email": "email",
        "phone_number": "phone_number",
        "people": "user_ids",
        "relation": "page_ids",
    }
    owned = field_for_type.get(value.type)
    if owned is None:
        raise ValueError("property type is not writable")
    for name in (
        "text",
        "number",
        "checkbox",
        "option",
        "options",
        "date",
        "url",
        "email",
        "phone_number",
        "user_ids",
        "page_ids",
    ):
        setting = getattr(value, name)
        is_set = setting is not None if name in ("number", "checkbox", "date") else bool(setting)
        if is_set and name != owned:
            raise ValueError(f"a {value.type} value cannot set {name}")
    if value.type in ("title", "rich_text"):
        return {value.type: _rich_text(value.text, 20000)}
    if value.type == "number":
        if value.number is not None and not math.isfinite(value.number):
            raise ValueError("a number must be finite")
        return {"number": value.number}
    if value.type == "checkbox":
        if value.checkbox is None:
            raise ValueError("a checkbox needs true or false")
        return {"checkbox": value.checkbox}
    if value.type in ("select", "status"):
        if not value.option:
            if value.type == "status":
                raise ValueError("a status cannot be cleared")
            return {"select": None}
        return {value.type: {"name": _option(value.option)}}
    if value.type == "multi_select":
        if len(value.options) > 100:
            raise ValueError("multi_select holds at most 100 options")
        options = [_option(item) for item in value.options]
        if len(set(options)) != len(options):
            raise ValueError("option is repeated")
        return {"multi_select": [{"name": item} for item in options]}
    if value.type == "date":
        return {"date": _date(value.date) if value.date else None}
    if value.type in ("url", "email", "phone_number"):
        raw_text = getattr(value, "phone_number" if value.type == "phone_number" else value.type)
        if raw_text == "":
            return {value.type: None}
        text = raw_text.strip()
        if value.type == "url":
            parsed = urlsplit(text)
            if not parsed.scheme or not parsed.netloc or len(text) > 2000:
                raise ValueError("url must be absolute and at most 2000 characters")
        if value.type == "email":
            display_name, address = parseaddr(text)
            local, separator, domain = text.partition("@")
            forbidden = '()<>[]:;@\\,"'

            def is_atom(part: str) -> bool:
                return (
                    bool(part)
                    and not part.startswith(".")
                    and not part.endswith(".")
                    and ".." not in part
                    and all(ord(character) >= 33 and character not in forbidden for character in part)
                )

            if (
                display_name or address != text or not separator or not is_atom(local) or not is_atom(domain)
                or len(text.encode()) > 200
            ):
                raise ValueError("email must be one plain email address")
        if value.type == "phone_number" and (len(text) > 200 or "\n" in text or "\r" in text):
            raise ValueError("phone number must be one line")
        return {value.type: text}
    if value.type == "people":
        return {"people": [{"object": "user", "id": item} for item in _ids(value.user_ids, "userIds")]}
    return {"relation": [{"id": item} for item in _ids(value.page_ids, "pageIds")]}


def encode_properties(values: dict[str, PropertyValue]) -> dict[str, object]:
    """Encode at most 100 property names and their typed values."""
    if len(values) > 100:
        raise ValueError("properties holds at most 100 values")
    result: dict[str, object] = {}
    for key in sorted(values):
        if not key.strip() or key.strip() != key or len(key) > 256 or "\n" in key or "\r" in key:
            raise ValueError("property key is invalid")
        result[key] = encode_property(values[key])
    return result


def encode_paragraphs(text: str) -> list[dict[str, object]]:
    """Split one plain text body at blank lines into Notion paragraph blocks."""
    if len(text) > 50000:
        raise ValueError("bodyText is at most 50000 characters")
    content = text.replace("\r\n", "\n").strip()
    if not content:
        return []
    paragraphs = _SEPARATOR.split(content)
    if len(paragraphs) > 100:
        raise ValueError("bodyText holds at most 100 paragraphs")
    return [
        {
            "object": "block",
            "type": "paragraph",
            "paragraph": {"rich_text": _rich_text(item.rstrip(" \t\n"), 50000)},
        }
        for item in paragraphs
    ]


_TEXT_TYPES = {"title", "rich_text", "url", "email", "phone_number"}
_TEXT_CONDITIONS = {"equals", "does_not_equal", "contains", "does_not_contain", "starts_with", "ends_with"}
_NUMBER_CONDITIONS = {
    "equals",
    "does_not_equal",
    "greater_than",
    "less_than",
    "greater_than_or_equal_to",
    "less_than_or_equal_to",
}
_DATE_CONDITIONS = {"equals", "before", "after", "on_or_before", "on_or_after"}
_RELATIVE_CONDITIONS = {
    "past_week",
    "past_month",
    "past_year",
    "next_week",
    "next_month",
    "next_year",
    "this_week",
}


def encode_filter(value: QueryFilter) -> dict[str, object]:
    """Encode a typed property, timestamp, or compound filter."""
    condition_count = [0]

    def encode_node(node: QueryFilter, depth: int) -> dict[str, object]:
        if isinstance(node, dict):
            node = QueryFilter(
                **{
                    ("and_" if key == "and" else "or_" if key == "or" else key): setting
                    for key, setting in node.items()
                }
            )
        compound = bool(node.and_ or node.or_)
        leaf = bool(node.property or node.timestamp)
        if compound == leaf or (node.and_ and node.or_) or (node.property and node.timestamp):
            raise ValueError("filter sets exactly one of property, timestamp, and, or or")
        if compound:
            if (
                depth >= 2
                or node.type
                or node.condition
                or any((node.text, node.number is not None, node.checkbox is not None, node.date, node.id))
            ):
                raise ValueError("invalid compound filter")
            key, children = ("and", node.and_) if node.and_ else ("or", node.or_)
            if not 1 <= len(children) <= 100:
                raise ValueError("compound filter holds 1 to 100 filters")
            return {key: [encode_node(child, depth + 1) for child in children]}
        condition_count[0] += 1
        if condition_count[0] > 100:
            raise ValueError("filter holds at most 100 conditions")
        condition = node.condition or ""
        if node.timestamp:
            if node.timestamp not in ("created_time", "last_edited_time") or node.type:
                raise ValueError("invalid timestamp filter")
            property_type = "date"
            wrapper: dict[str, object] = {"timestamp": node.timestamp}
            field = node.timestamp
        else:
            if (
                not node.property.strip()
                or node.property != node.property.strip()
                or len(node.property) > 256
            ):
                raise ValueError("invalid filter property")
            property_type = node.type or ""
            wrapper = {"property": node.property}
            field = property_type
        if property_type in _TEXT_TYPES:
            accepted = _TEXT_CONDITIONS | {"is_empty", "is_not_empty"}
            expected = "text"
        elif property_type in ("number", "unique_id"):
            accepted = _NUMBER_CONDITIONS | (
                {"is_empty", "is_not_empty"} if property_type == "number" else set()
            )
            expected = "number"
        elif property_type == "checkbox":
            accepted, expected = {"equals", "does_not_equal"}, "checkbox"
        elif property_type in ("select", "status"):
            accepted, expected = {"equals", "does_not_equal", "is_empty", "is_not_empty"}, "text"
        elif property_type == "multi_select":
            accepted, expected = {"contains", "does_not_contain", "is_empty", "is_not_empty"}, "text"
        elif property_type in ("people", "relation", "created_by", "last_edited_by"):
            accepted, expected = {"contains", "does_not_contain", "is_empty", "is_not_empty"}, "id"
        elif property_type in ("date", "created_time", "last_edited_time"):
            accepted = (
                _DATE_CONDITIONS
                | _RELATIVE_CONDITIONS
                | ({"is_empty", "is_not_empty"} if not node.timestamp else set())
            )
            expected = "date"
        elif property_type == "files":
            accepted, expected = {"is_empty", "is_not_empty"}, ""
        else:
            raise ValueError("unsupported filter property type")
        if condition not in accepted:
            raise ValueError("condition is not supported for property type")
        given = [
            name
            for name in ("text", "number", "checkbox", "date", "id")
            if getattr(node, name) is not None and getattr(node, name) != ""
        ]
        if condition in {"is_empty", "is_not_empty"} | _RELATIVE_CONDITIONS:
            if given:
                raise ValueError("condition takes no value")
            encoded: object = {} if condition in _RELATIVE_CONDITIONS else True
        else:
            if given != [expected]:
                raise ValueError("condition takes exactly one typed value")
            encoded = getattr(node, expected)
            if expected == "text" and (not str(encoded).strip() or len(str(encoded)) > 2000):
                raise ValueError("filter text is invalid")
            if expected == "number" and not math.isfinite(cast(float, encoded)):
                raise ValueError("filter number must be finite")
            if expected == "date":
                encoded = _date_text(str(encoded))
            if expected == "id":
                encoded = parse_id(str(encoded))
        wrapper[field] = {condition: encoded}
        return wrapper

    return encode_node(value, 0)


def encode_sorts(sorts: tuple[QuerySort, ...]) -> list[dict[str, str]]:
    """Encode up to ten Notion sort rules."""
    if len(sorts) > 10:
        raise ValueError("query holds at most 10 sorts")
    result: list[dict[str, str]] = []
    for sort in sorts:
        if isinstance(sort, dict):
            sort = QuerySort(**sort)
        direction = sort.direction or "ascending"
        if direction not in ("ascending", "descending") or bool(sort.property) == bool(sort.timestamp):
            raise ValueError("sort needs one property or timestamp and valid direction")
        if sort.timestamp:
            if sort.timestamp not in ("created_time", "last_edited_time"):
                raise ValueError("invalid timestamp sort")
            result.append({"timestamp": sort.timestamp, "direction": direction})
        else:
            if sort.property != sort.property.strip() or len(sort.property) > 256:
                raise ValueError("invalid property sort")
            result.append({"property": sort.property, "direction": direction})
    return result
