# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Convert Notion's REST objects into generated connector models."""

import re
from typing import Any, cast
from urllib.parse import urlsplit

from .models import DateRange, Page, PageProperty, Parent, SearchMatch

_DASHED_ID = re.compile(r"^[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}$")
_COMPACT_ID = re.compile(r"^[0-9a-fA-F]{32}$")
_TRAILING_ID = re.compile(r"([0-9a-fA-F]{32})$")


def parse_id(value: str) -> str:
    """Accept a Notion UUID or a URL ending in a UUID and return its dashed form."""
    text = value.strip()
    if not text or len(text.encode()) > 2048:
        raise ValueError("a Notion ID or URL is required and at most 2048 bytes")
    if text.lower().startswith("https://"):
        parsed = urlsplit(text)
        hostname = (parsed.hostname or "").lower()
        if not any(
            hostname == host or hostname.endswith("." + host)
            for host in ("notion.so", "notion.com", "notion.site")
        ):
            raise ValueError("Notion URL host is invalid")
        text = parsed.path.rstrip("/").rsplit("/", 1)[-1]
        if not _DASHED_ID.fullmatch(text):
            match = _TRAILING_ID.search(text.replace("-", ""))
            if match is None:
                raise ValueError("Notion URL does not end in an ID")
            text = match.group(1)
    if _DASHED_ID.fullmatch(text):
        return text.lower()
    if not _COMPACT_ID.fullmatch(text):
        raise ValueError("Notion ID must contain 32 hexadecimal characters")
    text = text.lower()
    return f"{text[:8]}-{text[8:12]}-{text[12:16]}-{text[16:20]}-{text[20:]}"


def canonical_or_raw(value: str) -> str:
    """Keep future provider IDs when they are not UUIDs."""
    try:
        return parse_id(value)
    except ValueError:
        return value


def decode_parent(raw: dict[str, object]) -> Parent:
    """Read the parent ID associated with Notion's parent type."""
    parent_type = str(raw.get("type", ""))
    field = {
        "data_source_id": "data_source_id",
        "database_id": "database_id",
        "page_id": "page_id",
        "block_id": "block_id",
    }.get(parent_type, "")
    return Parent(
        type=parent_type,
        id=canonical_or_raw(str(raw.get(field, ""))) if field else "",
        database_id=canonical_or_raw(str(raw.get("database_id", "")))
        if parent_type == "data_source_id"
        else "",
    )


def _plain_text(items: object) -> str:
    if not isinstance(items, list):
        return ""
    return "".join(str(item.get("plain_text", "")) for item in items if isinstance(item, dict))


def _date(raw: object) -> tuple[DateRange | None, str]:
    if not isinstance(raw, dict) or not raw.get("start"):
        return None, ""
    start, end = str(raw["start"]), str(raw.get("end") or "")
    return DateRange(start, end, str(raw.get("time_zone") or "")), start + ("/" + end if end else "")


def _number_text(number: object) -> str:
    if number is None:
        return ""
    if isinstance(number, float) and number.is_integer():
        return str(int(number))
    return str(number)


def decode_property(raw: dict[str, object], can_nest: bool = True) -> PageProperty:
    """Decode one typed page property without exposing provider message text."""
    property_type = str(raw.get("type", ""))
    fields: dict[str, object] = {"id": str(raw.get("id", "")), "type": property_type}
    plain = ""
    value = raw.get(property_type)
    if property_type in ("title", "rich_text"):
        plain = _plain_text(value)
        fields["text"] = plain
    elif property_type == "number":
        fields["number"] = value
        plain = _number_text(value)
    elif property_type == "checkbox":
        fields["checkbox"] = value
        plain = str(value).lower() if value is not None else ""
    elif property_type in ("select", "status"):
        plain = str(value.get("name", "")) if isinstance(value, dict) else ""
        fields["option"] = plain
    elif property_type == "multi_select":
        options = (
            tuple(str(item.get("name", "")) for item in value if isinstance(item, dict))
            if isinstance(value, list)
            else ()
        )
        fields["options"] = options
        plain = ", ".join(options)
    elif property_type in ("date", "created_time", "last_edited_time"):
        if property_type != "date":
            value = {"start": value}
        fields["date"], plain = _date(value)
    elif property_type in ("url", "email", "phone_number"):
        fields["phone_number" if property_type == "phone_number" else property_type] = str(value or "")
        plain = str(value or "")
    elif property_type in ("people", "created_by", "last_edited_by"):
        people = value if isinstance(value, list) else ([value] if isinstance(value, dict) else [])
        fields["user_ids"] = tuple(str(person.get("id", "")) for person in people if isinstance(person, dict))
        plain = ", ".join(
            str(person.get("name") or person.get("id", "")) for person in people if isinstance(person, dict)
        )
    elif property_type == "relation":
        page_ids = (
            tuple(str(page.get("id", "")) for page in value if isinstance(page, dict))
            if isinstance(value, list)
            else ()
        )
        fields["page_ids"] = page_ids
        plain = ", ".join(page_ids)
        fields["truncated"] = bool(raw.get("has_more"))
    elif property_type == "formula" and isinstance(value, dict):
        formula_type = value.get("type")
        formula_value = value.get(str(formula_type))
        if formula_type == "string":
            plain = str(formula_value or "")
            fields["text"] = plain
        elif formula_type == "number":
            fields["number"] = formula_value
            plain = _number_text(formula_value)
        elif formula_type == "boolean":
            fields["checkbox"] = formula_value
            plain = str(formula_value).lower() if formula_value is not None else ""
        elif formula_type == "date":
            fields["date"], plain = _date(formula_value)
    elif property_type == "rollup" and isinstance(value, dict):
        rollup_type = value.get("type")
        rollup_value = value.get(str(rollup_type))
        if rollup_type == "number":
            fields["number"] = rollup_value
            plain = _number_text(rollup_value)
        elif rollup_type == "date":
            fields["date"], plain = _date(rollup_value)
        elif rollup_type == "array" and can_nest and isinstance(rollup_value, list):
            fields["truncated"] = len(rollup_value) > 25
            plain = ", ".join(
                text
                for item in rollup_value[:25]
                if isinstance(item, dict)
                if (text := decode_property(item, False).plain_text)
            )
    elif property_type == "files":
        plain = (
            ", ".join(str(item.get("name", "")) for item in value if isinstance(item, dict))
            if isinstance(value, list)
            else ""
        )
    elif property_type == "unique_id" and isinstance(value, dict) and value.get("number") is not None:
        plain = str(value["number"])
        if value.get("prefix"):
            plain = str(value["prefix"]) + "-" + plain
        fields["text"] = plain
        fields["number"] = float(value["number"])
    elif property_type == "verification" and isinstance(value, dict):
        plain = str(value.get("state", ""))
    fields["plain_text"] = plain
    return PageProperty(**cast(Any, fields))


def decode_page(raw: object) -> Page:
    """Validate a Notion page and map its display properties."""
    if not isinstance(raw, dict) or raw.get("object") != "page":
        raise ValueError("Notion returned an unusable page")
    try:
        identifier = parse_id(str(raw.get("id", "")))
        properties_raw = raw.get("properties")
        if properties_raw is None:
            properties_raw = {}
        if not isinstance(properties_raw, dict):
            raise ValueError("invalid properties")
        if any(not isinstance(item, dict) for item in properties_raw.values()):
            raise ValueError("invalid property object")
        properties = {
            str(name): decode_property(item)
            for name, item in properties_raw.items()
        }
        parent_raw = raw.get("parent") or {}
        if not isinstance(parent_raw, dict):
            parent_raw = {}
    except (TypeError, ValueError) as error:
        raise ValueError("Notion returned an unusable page") from error
    title = next((item.text for item in properties.values() if item.type == "title"), "")
    return Page(
        id=identifier,
        url=str(raw.get("url", "")),
        title=title,
        parent=decode_parent(parent_raw),
        created_time=str(raw.get("created_time", "")),
        last_edited_time=str(raw.get("last_edited_time", "")),
        in_trash=bool(raw.get("in_trash")),
        archived=bool(raw.get("is_archived")),
        properties=properties,
    )


def decode_search_match(raw: dict[str, object]) -> SearchMatch | None:
    """Skip unknown object kinds and read known title matches."""
    object_type = str(raw.get("object", ""))
    if object_type not in ("page", "data_source"):
        return None
    if object_type == "page":
        properties = raw.get("properties") or {}
        if not isinstance(properties, dict):
            raise ValueError("Notion returned an invalid search page")
        title = next(
            (
                item.text
                for item in (
                    decode_property(value) for value in properties.values() if isinstance(value, dict)
                )
                if item.type == "title"
            ),
            "",
        )
    else:
        title = _plain_text(raw.get("title"))
    parent = raw.get("parent") or {}
    return SearchMatch(
        object_type=object_type,
        id=parse_id(str(raw.get("id", ""))),
        title=title,
        url=str(raw.get("url", "")),
        parent=decode_parent(parent if isinstance(parent, dict) else {}),
        last_edited_time=str(raw.get("last_edited_time", "")),
        in_trash=bool(raw.get("in_trash")),
    )
