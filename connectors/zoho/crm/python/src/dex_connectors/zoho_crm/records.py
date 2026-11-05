# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Decode Zoho record, metadata, and write responses without exposing provider text."""

import json
import math
import re
from datetime import datetime
from decimal import Decimal
from typing import Any

from .coql import FIELD, FIELD_PATH, MODULE, RECORD_ID
from .models import ModuleField, PicklistValue, ProviderError, Record


class InvalidFieldNumber(ValueError):
    """A provider number cannot survive the Python SDK JSON codec unchanged."""


def _exact_float(literal: str) -> float:
    number = float(literal)
    if not math.isfinite(number) or Decimal(repr(number)) != Decimal(literal):
        raise InvalidFieldNumber("record field contains a number that cannot be preserved exactly")
    return number


def decode_json(body: bytes) -> Any:
    """Parse provider JSON and reject numeric values that would lose digits."""
    return json.loads(
        body, parse_float=_exact_float, parse_constant=lambda _: (_ for _ in ()).throw(ValueError())
    )


def decode_record(module: str, document: Any) -> Record:
    """Keep exact field values and omit Zoho system keys beginning with $."""
    if not isinstance(document, dict):
        raise ValueError("a record is not a JSON object")
    record_id = document.get("id")
    if not isinstance(record_id, str) or not RECORD_ID.fullmatch(record_id):
        raise ValueError("a record has no numeric id")
    fields = {name: value for name, value in document.items() if name != "id" and FIELD_PATH.fullmatch(name)}
    return Record(module, record_id, fields)


def decode_page(module: str, body: bytes) -> tuple[tuple[Record, ...], bool]:
    """Read a record page; blank body is an empty page."""
    if not body.strip():
        return (), False
    try:
        document = decode_json(body)
        if not isinstance(document, dict):
            raise ValueError
        data = document.get("data")
        if data is None:
            data = []
        if not isinstance(data, list):
            raise ValueError
        info = document.get("info")
        if info is None:
            info = {}
        if not isinstance(info, dict) or type(info.get("more_records", False)) is not bool:
            raise ValueError
        return tuple(decode_record(module, item) for item in data), info.get("more_records", False)
    except (TypeError, UnicodeDecodeError, json.JSONDecodeError) as error:
        raise ValueError("the page is not a JSON object with a data list") from error


def provider_errors(code: str, field: str, duplicate_id: str) -> tuple[ProviderError, ...]:
    """Return machine-readable provider error details."""
    return (ProviderError(code=code, field=field, duplicate_record_id=duplicate_id),) if code else ()


def _optional_time(text: Any) -> str:
    if not isinstance(text, str):
        return "0001-01-01T00:00:00Z"
    try:
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(?:\.\d+)?(?:Z|[+-]\d{2}:\d{2})", text):
            return "0001-01-01T00:00:00Z"
        datetime.fromisoformat(text.replace("Z", "+00:00"))
        return text
    except ValueError:
        return "0001-01-01T00:00:00Z"


def decode_write(body: bytes) -> tuple[str, str, str, str, str, Any]:
    """Decode one successful write or return its record error object."""
    try:
        document = decode_json(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("the response is not a JSON object with a data list") from error
    if not isinstance(document, dict) or not isinstance(document.get("data"), list):
        raise ValueError("the response is not a JSON object with a data list")
    data = document["data"]
    if len(data) != 1:
        raise ValueError(f"the response holds {len(data)} results for one record")
    item = data[0]
    if isinstance(item, dict) and item.get("status") == "error":
        return "", "", "", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z", item
    if not isinstance(item, dict) or item.get("code") != "SUCCESS":
        raise ValueError("the result is not a SUCCESS entry")
    details = item.get("details")
    if (
        not isinstance(details, dict)
        or not isinstance(details.get("id"), str)
        or not RECORD_ID.fullmatch(details["id"])
    ):
        raise ValueError("the result has no numeric record id")
    duplicate_field = item.get("duplicate_field")
    if not isinstance(duplicate_field, str) or not FIELD.fullmatch(duplicate_field):
        duplicate_field = ""
    action = item.get("action")
    return (
        action if isinstance(action, str) else "",
        duplicate_field,
        details["id"],
        _optional_time(details.get("Created_Time")),
        _optional_time(details.get("Modified_Time")),
        None,
    )


def decode_module_fields(body: bytes) -> tuple[ModuleField, ...]:
    """Decode field metadata, enforcing Go integer and required-field rules."""
    try:
        document = decode_json(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise ValueError("the response is not a JSON object with a fields list") from error
    if not isinstance(document, dict) or not isinstance(document.get("fields"), list):
        raise ValueError("the response is not a JSON object with a fields list")
    result: list[ModuleField] = []
    for wire in document["fields"]:
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("api_name"), str)
            or not FIELD.fullmatch(wire["api_name"])
            or not isinstance(wire.get("data_type"), str)
            or not wire["data_type"]
        ):
            raise ValueError("a field has no valid api_name or data_type")
        length = wire.get("length", 0)
        if type(length) is not int or not -(2**63) <= length < 2**63:
            raise ValueError("field length is not an integer")
        lookup = wire.get("lookup")
        module = lookup.get("module") if isinstance(lookup, dict) else None
        module_name = module.get("api_name") if isinstance(module, dict) else ""
        if not isinstance(module_name, str) or not MODULE.fullmatch(module_name):
            module_name = ""
        picklist: list[PicklistValue] = []
        values = wire.get("pick_list_values") or []
        if not isinstance(values, list):
            raise ValueError("pick_list_values is not a list")
        for value in values:
            if not isinstance(value, dict):
                raise ValueError("a picklist value is not an object")
            picklist.append(
                PicklistValue(
                    actual_value=value.get("actual_value", ""),
                    display_value=value.get("display_value", ""),
                    is_unused=value.get("type") == "unused",
                )
            )
        unique = wire.get("unique")
        result.append(
            ModuleField(
                api_name=wire["api_name"],
                display_label=wire.get("display_label", ""),
                data_type=wire["data_type"],
                is_custom=wire.get("custom_field", False),
                is_read_only=wire.get("read_only", False),
                is_system_mandatory=wire.get("system_mandatory", False),
                is_unique=isinstance(unique, dict) and bool(unique),
                length=max(length, 0),
                lookup_module=module_name,
                picklist_values=tuple(picklist),
            )
        )
    return tuple(result)
