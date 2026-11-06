# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""monday.com item decoding and typed column request values."""

import json
import math
import re
from datetime import UTC, datetime
from decimal import Decimal
from email.utils import parseaddr
from urllib.parse import urlsplit

from .models import ColumnValue, Item, ItemColumnValue, ItemGroup

NUMERIC_ID = re.compile(r"[1-9][0-9]{0,19}\Z")
COLUMN_ID = re.compile(r"[A-Za-z0-9_-]{1,100}\Z")
COLUMN_TYPE = re.compile(r"[a-z][a-z0-9_]{0,63}\Z")


def numeric_id(value: str, field: str) -> str:
    """Validate a provider decimal identifier while retaining Go's whitespace trimming."""
    trimmed = value.strip()
    if not NUMERIC_ID.fullmatch(trimmed):
        raise ValueError(f"{field} must be a numeric monday.com ID such as 1234567890")
    return trimmed


def column_ids(values: tuple[str, ...]) -> list[str] | None:
    """Validate and deduplicate requested column IDs in their original order."""
    if not values:
        return None
    if len(values) > 50:
        raise ValueError("columnIds accepts at most 50 column IDs")
    if any(not COLUMN_ID.fullmatch(value) for value in values):
        raise ValueError("columnIds must be monday.com column IDs such as status or date4")
    return list(dict.fromkeys(values))


def _time(value: object) -> datetime:
    if not isinstance(value, str) or not value:
        return datetime.min.replace(tzinfo=UTC)
    for candidate in (value, value.replace("Z", "+00:00")):
        try:
            return datetime.fromisoformat(candidate).astimezone(UTC)
        except ValueError:
            continue
    for pattern in ("%Y-%m-%d %H:%M:%S %Z", "%Y-%m-%d %H:%M:%S"):
        try:
            return datetime.strptime(value, pattern).replace(tzinfo=UTC)
        except ValueError:
            continue
    return datetime.min.replace(tzinfo=UTC)


def _string(resource: dict[str, object], key: str) -> str:
    value = resource.get(key, "")
    if not isinstance(value, str):
        raise ValueError(f"{key} is not a string")
    return value


def decode_item(resource: object) -> Item:
    """Decode and validate one item, bounding raw column values."""
    if not isinstance(resource, dict):
        raise ValueError("item is not an object")
    item_id = _string(resource, "id")
    if not NUMERIC_ID.fullmatch(item_id):
        raise ValueError("item ID is not a monday.com numeric ID")
    name = _string(resource, "name")
    state = _string(resource, "state")
    if state not in ("", "active", "archived", "deleted"):
        raise ValueError("item state is not a monday.com state")
    board_id = ""
    board = resource.get("board")
    if board is not None:
        if not isinstance(board, dict) or not NUMERIC_ID.fullmatch(_string(board, "id")):
            raise ValueError("item board ID is not a monday.com numeric ID")
        board_id = _string(board, "id")
    group = None
    group_resource = resource.get("group")
    if group_resource is not None:
        if not isinstance(group_resource, dict) or not COLUMN_ID.fullmatch(_string(group_resource, "id")):
            raise ValueError("item group ID is not a monday.com group ID")
        group = ItemGroup(_string(group_resource, "id"), _string(group_resource, "title"))
    creator_id = ""
    if resource.get("creator_id") is not None:
        creator_id = _string(resource, "creator_id")
        if creator_id and not NUMERIC_ID.fullmatch(creator_id):
            raise ValueError("item creator ID is not a monday.com numeric ID")
    url = _string(resource, "url")
    parsed = urlsplit(url)
    if parsed.scheme != "https" or not parsed.netloc or parsed.username is not None:
        url = ""
    column_values_resource = resource.get("column_values") or []
    if not isinstance(column_values_resource, list):
        raise ValueError("column values are not a list")
    column_values = tuple(decode_column_value(value) for value in column_values_resource)
    return Item(
        item_id,
        name,
        state or None,
        board_id,
        group,
        creator_id,
        url,
        _time(resource.get("created_at")),
        _time(resource.get("updated_at")),
        column_values,
    )


def decode_column_value(resource: object) -> ItemColumnValue:
    """Decode one typed display value with Go's 4 KiB and 16 KiB bounds."""
    if not isinstance(resource, dict):
        raise ValueError("column is not an object")
    column_id = _string(resource, "id")
    column_type = _string(resource, "type")
    if not COLUMN_ID.fullmatch(column_id):
        raise ValueError("column ID is not a monday.com column ID")
    if not COLUMN_TYPE.fullmatch(column_type):
        raise ValueError(f"column {column_id} type is not a monday.com column type")
    text = resource.get("text") or ""
    if not isinstance(text, str):
        raise ValueError("column text is not a string")
    encoded_text = text.encode()
    truncated = len(encoded_text) > 4096
    if truncated:
        text = encoded_text[:4096].decode(errors="ignore")
    raw = resource.get("value")
    if isinstance(raw, str):
        if raw.strip() in ("", "null"):
            raw = None
        else:
            try:
                raw = json.loads(raw)
            except json.JSONDecodeError as error:
                raise ValueError(f"column {column_id} value is not JSON") from error
    if raw is None:
        return ItemColumnValue(column_id, text=text, value=None, text_truncated=truncated, type=column_type)
    raw_json = go_json(raw)
    if len(raw_json.encode()) > 16384:
        return ItemColumnValue(
            column_id, text=text, value=None, text_truncated=truncated, value_omitted=True, type=column_type
        )
    return ItemColumnValue(column_id, text=text, value=raw, text_truncated=truncated, type=column_type)


def go_json(value: object) -> str:
    """Encode nested GraphQL JSON strings with Go's HTML escaping and key order."""
    encoded = json.dumps(value, ensure_ascii=False, separators=(",", ":"), sort_keys=True)
    return (
        encoded.replace("<", "\\u003c")
        .replace(">", "\\u003e")
        .replace("&", "\\u0026")
        .replace("\u2028", "\\u2028")
        .replace("\u2029", "\\u2029")
    )


def _calendar_date(value: str) -> None:
    if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value):
        raise ValueError("date must be a YYYY-MM-DD date such as 2026-02-18")
    try:
        datetime.strptime(value, "%Y-%m-%d")
    except ValueError as error:
        raise ValueError("date must be a real calendar date") from error


def _label(value: str, name: str) -> str:
    if not value.strip() or len(value) > 255 or any(ord(char) < 32 or ord(char) == 127 for char in value):
        raise ValueError(f"{name} must be one nonblank line of at most 255 characters")
    return value


def encode_column_values(values: dict[str, ColumnValue]) -> str:
    """Encode the exact typed column object sent as a GraphQL JSON string."""
    if len(values) > 50:
        raise ValueError("columnValues holds at most 50 columns")
    result: dict[str, object] = {}
    for column_id, value in sorted(values.items()):
        if column_id == "name" or not COLUMN_ID.fullmatch(column_id):
            raise ValueError("columnValues keys must be monday.com column IDs other than name")
        if isinstance(value, dict):
            from dataclasses import fields

            names = {
                field.metadata.get("dex_json_name", field.name): field.name for field in fields(ColumnValue)
            }
            value = ColumnValue(**{names[key]: item for key, item in value.items()})
        result[column_id] = encode_column_value(value)
    return go_json(result)


def encode_column_value(value: ColumnValue) -> object:
    """Encode one supported monday.com column value after type-specific validation."""
    fields = {
        "text": ("text",),
        "long_text": ("text",),
        "numbers": ("number",),
        "status": ("status_label", "status_index"),
        "date": ("date", "time"),
        "people": ("person_ids", "team_ids"),
        "checkbox": ("checked",),
        "email": ("email", "text"),
        "link": ("url", "text"),
        "phone": ("phone", "country_code"),
        "dropdown": ("dropdown_labels", "dropdown_label_ids"),
        "timeline": ("date", "end_date"),
    }
    if value.type not in fields:
        raise ValueError("type must be a supported monday.com column type such as text, status, or date")
    for field_name in set(ColumnValue.__dataclass_fields__) - {"type", "cleared"} - set(fields[value.type]):
        current = getattr(value, field_name)
        if current is not None and current != "" and current != ():
            raise ValueError(f"{field_name} does not apply to a {value.type} column")
    if value.cleared:
        if any(getattr(value, field_name) not in (None, "", ()) for field_name in fields[value.type]):
            raise ValueError("a cleared value cannot set another field")
        return None
    if value.type in ("text", "long_text"):
        if not value.text or len(value.text) > 2000:
            raise ValueError("a text value needs text of at most 2000 characters")
        return value.text if value.type == "text" else {"text": value.text}
    if value.type == "numbers":
        if value.number is None or not math.isfinite(value.number):
            raise ValueError("a numbers value needs a finite number")
        return format(Decimal(repr(value.number)), "f")
    if value.type == "status":
        if (value.status_label == "") == (value.status_index is None):
            raise ValueError("a status value sets exactly one of statusLabel and statusIndex")
        if value.status_index is not None:
            if value.status_index < 0:
                raise ValueError("statusIndex cannot be negative")
            return {"index": value.status_index}
        return {"label": _label(value.status_label, "statusLabel")}
    if value.type == "date":
        _calendar_date(value.date)
        result = {"date": value.date}
        if value.time:
            if not re.fullmatch(r"\d{2}:\d{2}:\d{2}", value.time):
                raise ValueError("time must be an HH:MM:SS UTC time such as 09:00:00")
            try:
                datetime.strptime(value.time, "%H:%M:%S")
            except ValueError as error:
                raise ValueError("time must be an HH:MM:SS UTC time such as 09:00:00") from error
            result["time"] = value.time
        return result
    if value.type == "people":
        if not 1 <= len(value.person_ids) + len(value.team_ids) <= 50:
            raise ValueError("a people value lists 1 to 50 people and teams")
        if any(identifier <= 0 for identifier in (*value.person_ids, *value.team_ids)):
            raise ValueError("people and team IDs must be positive monday.com IDs")
        return {
            "personsAndTeams": [
                {"id": identifier, "kind": kind}
                for kind, identifiers in (("person", value.person_ids), ("team", value.team_ids))
                for identifier in identifiers
            ]
        }
    if value.type == "checkbox":
        if value.checked is None:
            raise ValueError("a checkbox value needs checked")
        return {"checked": "true"} if value.checked else None
    if value.type == "email":
        display, address = parseaddr(value.email)
        if display or address != value.email or not address.partition("@")[0]:
            raise ValueError("email must be one bare address such as jane@example.com")
        return {"email": value.email, "text": _label(value.text, "text") if value.text else value.email}
    if value.type == "link":
        parsed = urlsplit(value.url)
        if parsed.scheme not in ("https", "http") or not parsed.netloc or len(value.url) > 2000:
            raise ValueError("url must be an absolute http or https URL")
        return {"url": value.url, "text": _label(value.text, "text") if value.text else value.url}
    if value.type == "phone":
        if not re.fullmatch(r"\+?[0-9]{4,20}", value.phone) or not re.fullmatch(
            r"[A-Z]{2}", value.country_code
        ):
            raise ValueError(
                "a phone value needs 4 to 20 digits with an optional + and a country code such as US"
            )
        return {"phone": value.phone, "countryShortName": value.country_code}
    if value.type == "dropdown":
        if (
            bool(value.dropdown_labels) == bool(value.dropdown_label_ids)
            or len(value.dropdown_labels) + len(value.dropdown_label_ids) > 50
        ):
            raise ValueError("a dropdown value sets exactly one of dropdownLabels and dropdownLabelIds")
        if value.dropdown_label_ids:
            if any(identifier < 0 for identifier in value.dropdown_label_ids):
                raise ValueError("dropdownLabelIds cannot be negative")
            return {"ids": list(value.dropdown_label_ids)}
        return {"labels": [_label(label, "dropdownLabels") for label in value.dropdown_labels]}
    _calendar_date(value.date)
    _calendar_date(value.end_date)
    if value.end_date < value.date:
        raise ValueError("a timeline ends on or after it starts")
    return {"from": value.date, "to": value.end_date}
