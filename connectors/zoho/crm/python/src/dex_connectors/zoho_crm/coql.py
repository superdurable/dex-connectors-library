# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Validate and render Zoho's COQL query subset."""

import re
from datetime import datetime

from .models import COQLValue, FindRecordsInput, RecordCondition, RecordSort

MODULE = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,99}\Z")
FIELD = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,99}\Z")
FIELD_PATH = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}\Z")
RECORD_ID = re.compile(r"[0-9]{1,20}\Z")
NUMBER = re.compile(r"-?[0-9]{1,18}(?:\.[0-9]{1,9})?\Z")
DATE = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}\Z")


def validate_module(module: str) -> None:
    """Require a Zoho module API name."""
    if not MODULE.fullmatch(module):
        raise ValueError("module must be a Zoho CRM module API name such as Contacts, Accounts, or Deals")


def validate_record_id(record_id: str) -> None:
    """Require a decimal record ID."""
    if not RECORD_ID.fullmatch(record_id):
        raise ValueError("recordId must be a numeric Zoho CRM record ID such as 4150868000000376008")


def validate_fields(fields: tuple[str, ...], minimum: int) -> None:
    """Require bounded, distinct field paths."""
    if not minimum <= len(fields) <= 50:
        raise ValueError(f"fields must name {minimum} to 50 field API names")
    seen: set[str] = set()
    for name in fields:
        if not FIELD_PATH.fullmatch(name) or name in seen:
            raise ValueError(
                f'field "{name}" must be a distinct Zoho CRM field API name such as Last_Name or Account_Name.Account_Name'
            )
        seen.add(name)


def quote_text(value: str) -> str:
    """Quote text that cannot escape a COQL literal."""
    if not value or len(value) > 1000:
        raise ValueError("a text value must be 1 to 1000 characters of valid UTF-8")
    if "'" in value or "\\" in value or any(ord(char) < 32 or 127 <= ord(char) <= 159 for char in value):
        raise ValueError(
            "a text value cannot contain an apostrophe, a backslash, or a control character, which COQL cannot quote safely"
        )
    return f"'{value}'"


def render_value(value: COQLValue) -> str:
    """Render one typed COQL value."""
    if value.kind == "text":
        return quote_text(value.text)
    if value.kind in ("textList", "recordIdList"):
        if not 1 <= len(value.texts) <= 100:
            raise ValueError("a list value must hold 1 to 100 entries")
        if value.kind == "recordIdList" and any(not RECORD_ID.fullmatch(item) for item in value.texts):
            raise ValueError("a record ID list may hold only numeric IDs")
        return "(" + ", ".join(quote_text(item) for item in value.texts) + ")"
    if value.kind == "number":
        if not NUMBER.fullmatch(value.text):
            raise ValueError("a number value must be a plain decimal such as 50000 or 1250.50")
        return value.text
    if value.kind == "boolean":
        return "true" if value.boolean else "false"
    if value.kind == "date":
        try:
            if not DATE.fullmatch(value.text):
                raise ValueError
            datetime.strptime(value.text, "%Y-%m-%d")
        except ValueError as error:
            raise ValueError("a date value must be YYYY-MM-DD") from error
        return f"'{value.text}'"
    if value.kind == "dateTime":
        try:
            if not re.fullmatch(
                r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}[+-][0-9]{2}:[0-9]{2}", value.text
            ):
                raise ValueError
            datetime.fromisoformat(value.text)
        except ValueError as error:
            raise ValueError("a datetime value must be built with COQLDateTime") from error
        return f"'{value.text}'"
    if value.kind == "recordId":
        if not RECORD_ID.fullmatch(value.text):
            raise ValueError("a record ID value must be numeric")
        return f"'{value.text}'"
    raise ValueError("value kind is not supported")


def render_condition(condition: RecordCondition) -> str:
    """Render one field comparator and value."""
    if isinstance(condition, dict):
        document = condition
        raw_value = document.get("value")
        value = COQLValue(**raw_value) if isinstance(raw_value, dict) else raw_value
        condition = RecordCondition(field=document.get("field", ""), operator=document.get("operator", ""), value=value)
    if not FIELD_PATH.fullmatch(condition.field):
        raise ValueError("field must be a Zoho CRM field API name such as Email or Account_Name.Account_Name")
    operator = condition.operator
    value = condition.value
    if operator in ("is null", "is not null"):
        if value is not None:
            raise ValueError(f"{operator} takes no value")
        return f"{condition.field} {operator}"
    if operator in ("in", "not in"):
        if value is None or value.kind not in ("textList", "recordIdList"):
            raise ValueError(f"{operator} needs a COQLTextList or COQLRecordIDList value")
    elif operator in ("=", "!="):
        if value is None or value.kind in ("textList", "recordIdList"):
            raise ValueError(f"{operator} needs one value, not a list")
    elif operator in (">", ">=", "<", "<="):
        if value is None or value.kind not in ("number", "date", "dateTime"):
            raise ValueError(f"{operator} needs a number, date, or datetime value")
    else:
        raise ValueError("operator must be one of = != > >= < <= in, not in, is null, or is not null")
    assert value is not None
    return f"{condition.field} {operator} {render_value(value)}"


def render_conditions(conditions: tuple[RecordCondition, ...]) -> str:
    """Join up to ten predicates with Zoho's nested `and` syntax."""
    if not 1 <= len(conditions) <= 10:
        raise ValueError("conditions must hold 1 to 10 conditions")
    result = ""
    for index, condition in enumerate(conditions):
        try:
            part = render_condition(condition)
        except ValueError as error:
            raise ValueError(f"condition {index + 1}: {error}") from error
        result = part if index == 0 else f"({result} and {part})"
    return result


def render_order(sort: RecordSort | None) -> str:
    """Apply a stable ID tie break to a requested sort."""
    if isinstance(sort, dict):
        sort = RecordSort(field=sort.get("field", ""), is_descending=sort.get("isDescending", False))
    if sort is None or sort.field == "id":
        return "id desc" if sort is not None and sort.is_descending else "id asc"
    if not FIELD_PATH.fullmatch(sort.field):
        raise ValueError("sort field must be a Zoho CRM field API name such as Modified_Time")
    return f"{sort.field} {'desc' if sort.is_descending else 'asc'}, id asc"


def select_query(
    module: str, fields: tuple[str, ...], condition: str, order: str, offset: int, limit: int
) -> str:
    """Build a single COQL select statement."""
    selected = [field for field in fields if field != "id"] or ["id"]
    return f"select {', '.join(selected)} from {module} where {condition} order by {order} limit {offset}, {limit}"


def find_records_query(input: FindRecordsInput) -> str:
    """Validate a findRecords input and render its COQL statement."""
    validate_module(input.module)
    validate_fields(input.fields, 1)
    limit = input.limit or 10
    if not 1 <= limit <= 200:
        raise ValueError("limit must be 1 to 200")
    if input.offset < 0 or input.offset + limit > 100000:
        raise ValueError("offset plus limit must not pass Zoho CRM's 100000-record COQL depth")
    condition = render_conditions(input.conditions)
    return select_query(input.module, input.fields, condition, render_order(input.sort), input.offset, limit)
