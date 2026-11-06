# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho CRM record operations with Go-compatible request and branch semantics."""

from __future__ import annotations

import asyncio
import json
import math
import re
from datetime import UTC, datetime
from decimal import Decimal
from typing import Any
from urllib.parse import quote, urlencode

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    QueryAttempt,
    Receipt,
)

from ._generated.definitions import (
    FIND_RECORDS_DEFINITION,
    GET_RECORD_DEFINITION,
    LIST_MODIFIED_RECORDS_DEFINITION,
    LIST_MODULE_FIELDS_DEFINITION,
    UPDATE_RECORD_DEFINITION,
    UPSERT_RECORD_DEFINITION,
)
from .client import _FIELD, _RECORD_ID, _REQUEST_REJECTION_CODES, Client, Exchange, failure
from .models import (
    COQLValue,
    FindRecordsInput,
    FindRecordsOutput,
    GetRecordInput,
    ListModifiedRecordsInput,
    ListModifiedRecordsOutput,
    ListModuleFieldsInput,
    ListModuleFieldsOutput,
    ModuleField,
    PicklistValue,
    Record,
    RecordCondition,
    RecordSort,
    UpdateRecordInput,
    UpdateRecordOutput,
    UpsertRecordInput,
    UpsertRecordOutput,
)

_FIELD_PATH = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}$")
_NUMBER = re.compile(r"^-?[0-9]{1,18}(?:\.[0-9]{1,9})?$")
_CURSOR = re.compile(r"^([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z)/([0-9]{1,20})$")


def _json_float(text: str) -> float:
    number = float(text)
    if not math.isfinite(number) or Decimal(str(number)) != Decimal(text):
        raise ValueError("Zoho CRM JSON number cannot be represented exactly")
    return number


def _json_constant(_: str) -> None:
    raise ValueError("Zoho CRM JSON contains a non-finite number")


def _strict_json(body: bytes) -> Any:
    return json.loads(body, parse_float=_json_float, parse_constant=_json_constant)


def _module(value: str) -> None:
    if not _FIELD.fullmatch(value):
        raise ValueError("module must be a Zoho CRM module API name such as Contacts, Accounts, or Deals")


def _record_id(value: str) -> None:
    if not _RECORD_ID.fullmatch(value):
        raise ValueError("recordId must be a numeric Zoho CRM record ID such as 4150868000000376008")


def _fields(values: tuple[str, ...], minimum: int, *, path: bool = True) -> None:
    if not minimum <= len(values) <= 50:
        raise ValueError(f"fields must name {minimum} to 50 field API names")
    seen: set[str] = set()
    for name in values:
        if not (_FIELD_PATH if path else _FIELD).fullmatch(name) or name in seen:
            raise ValueError(f"field {name!r} must be a distinct Zoho CRM field API name")
        seen.add(name)


def _decimal_id_after(left: str, right: str) -> bool:
    left, right = left.lstrip("0") or "0", right.lstrip("0") or "0"
    return (len(left), left) > (len(right), right)


def _zoho_time(value: str) -> datetime:
    if not isinstance(value, str) or not re.fullmatch(
        r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}"
        r"(?:\.[0-9]+)?(?:Z|[+-][0-9]{2}:[0-9]{2})", value
    ):
        raise ValueError("invalid Zoho CRM timestamp")
    instant = datetime.fromisoformat(value.replace("Z", "+00:00"))
    if instant.tzinfo is None:
        raise ValueError("invalid Zoho CRM timestamp")
    return instant.astimezone(UTC)


def _query_result(
    exchange: Exchange, call: Call, operation: str, zero: Any, *, not_found: bool = False
) -> QueryAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome == "retry":
        return QueryAttempt.retry(
            exchange.failure or failure(operation, FailureKind.TRANSPORT, "Zoho CRM request failed"),
            exchange.retry_after,
        )
    branch = (
        "defect"
        if exchange.outcome == "defect"
        else "invalidResponse"
        if exchange.outcome == "invalid"
        else "notFound"
        if not_found and exchange.summary.is_record_id_invalid
        else "providerRejected"
    )
    return QueryAttempt.branch_result(
        branch, zero, Receipt() if exchange.status == 0 else call_receipt(call, exchange), exchange.failure
    )


def _mutation_result(
    exchange: Exchange, call: Call, operation: str, zero: Any, *, not_found: bool = False
) -> MutationAttempt[Any] | None:
    if exchange.outcome == "succeeded":
        return None
    if exchange.outcome == "retry":
        return MutationAttempt.retry(
            exchange.failure or failure(operation, FailureKind.TRANSPORT, "Zoho CRM request failed"),
            exchange.retry_after,
        )
    if exchange.outcome == "defect":
        branch = "defect"
    elif exchange.outcome == "invalid":
        branch = "invalidResponse"
    elif exchange.summary.code == "DUPLICATE_DATA":
        branch = "conflict"
    elif not_found and exchange.summary.is_record_id_invalid:
        branch = "notFound"
    elif exchange.summary.code in _REQUEST_REJECTION_CODES or (
        exchange.status >= 300 and exchange.status not in (400, 422)
    ):
        branch = "providerRejected"
    else:
        branch = "recordRejected"
    return MutationAttempt.branch_result(
        branch, zero, Receipt() if exchange.status == 0 else call_receipt(call, exchange), exchange.failure
    )


def call_receipt(call: Call, exchange: Exchange, record_id: str = "") -> Receipt:
    """Record provider correlation without body or token values."""
    return Receipt(
        call.id,
        "zoho-crm",
        datetime.now(UTC),
        call.idempotency_key,
        record_id,
        metadata={"remainingCredits": exchange.remaining_credits} if exchange.remaining_credits else {},
    )


def _records(module: str, body: bytes) -> tuple[tuple[Record, ...], bool]:
    if not body.strip():
        return (), False
    document = _strict_json(body)
    if not isinstance(document, dict):
        raise ValueError("the page is not a JSON object with a data list")
    rows = document.get("data")
    if rows is None:
        rows = []
    if not isinstance(rows, list):
        raise ValueError("the page is not a JSON object with a data list")
    records: list[Record] = []
    for row in rows:
        if (
            not isinstance(row, dict)
            or not isinstance(row.get("id"), str)
            or not _RECORD_ID.fullmatch(row["id"])
        ):
            raise ValueError("a record has no numeric id")
        records.append(
            Record(
                module,
                row["id"],
                {key: value for key, value in row.items() if key != "id" and _FIELD_PATH.fullmatch(key)},
            )
        )
    info = document.get("info")
    if info is not None and not isinstance(info, dict):
        raise ValueError("the page is not a JSON object with a data list")
    more = info.get("more_records", False) if isinstance(info, dict) else False
    if type(more) is not bool:
        raise ValueError("the page is not a JSON object with a data list")
    return tuple(records), more


def _condition_value(value: COQLValue) -> str:
    if value.kind == "number":
        if not _NUMBER.fullmatch(value.text):
            raise ValueError("a number value must be a plain decimal")
        return value.text
    if value.kind == "boolean":
        return "true" if value.boolean else "false"
    if value.kind in ("date", "dateTime"):
        if value.kind == "date":
            if not re.fullmatch(r"[0-9]{4}-[0-9]{2}-[0-9]{2}", value.text):
                raise ValueError("a date value must be YYYY-MM-DD")
            datetime.strptime(value.text, "%Y-%m-%d")
        else:
            if not re.fullmatch(
                r"[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}(?:\.[0-9]+)?[+-][0-9]{2}:[0-9]{2}",
                value.text,
            ):
                raise ValueError("a datetime value must be built with COQLDateTime")
            _zoho_time(value.text)
        return "'" + value.text + "'"
    if value.kind in ("text", "recordId"):
        values: tuple[str, ...] = (value.text,)
    elif value.kind in ("textList", "recordIdList"):
        values = value.texts
        if not 1 <= len(values) <= 100:
            raise ValueError("a list value must hold 1 to 100 entries")
    else:
        raise ValueError("value kind is not supported")
    rendered = []
    for text in values:
        if value.kind.startswith("recordId") and not _RECORD_ID.fullmatch(text):
            raise ValueError("a record ID value must be numeric")
        if (
            not text
            or len(text) > 1000
            or "'" in text
            or "\\" in text
            or any(ord(c) < 32 or 127 <= ord(c) <= 159 for c in text)
        ):
            raise ValueError("a text value cannot contain an apostrophe, a backslash, or a control character")
        rendered.append("'" + text + "'")
    return "(" + ", ".join(rendered) + ")" if value.kind.endswith("List") else rendered[0]


def build_find_records_query(input: FindRecordsInput) -> str:
    """Render a validated COQL query with typed, safely quoted criteria."""
    _module(input.module)
    _fields(input.fields, 1)
    if type(input.limit) is not int or type(input.offset) is not int:
        raise ValueError("limit and offset must be integers")
    limit = input.limit or 10
    if (
        type(limit) is not int
        or not 1 <= limit <= 200
        or type(input.offset) is not int
        or input.offset < 0
        or input.offset + limit > 100000
    ):
        raise ValueError("invalid COQL page")
    if not 1 <= len(input.conditions) <= 10:
        raise ValueError("conditions must hold 1 to 10 conditions")
    rendered = ""
    for condition in input.conditions:
        if isinstance(condition, dict):
            raw_value = condition.get("value")
            if isinstance(raw_value, dict):
                raw_value = COQLValue(**raw_value)
            condition = RecordCondition(
                condition.get("field", ""), operator=condition.get("operator", ""), value=raw_value
            )
        if not _FIELD_PATH.fullmatch(condition.field):
            raise ValueError("field must be a Zoho CRM field API name")
        operator = condition.operator
        if operator in ("is null", "is not null"):
            if condition.value is not None:
                raise ValueError("null comparison takes no value")
            criterion = condition.field + " " + operator
        else:
            if condition.value is None or operator not in ("=", "!=", ">", ">=", "<", "<=", "in", "not in"):
                raise ValueError("invalid COQL comparator")
            if operator in ("in", "not in") and condition.value.kind not in ("textList", "recordIdList"):
                raise ValueError("list comparator needs a list")
            if operator not in ("in", "not in") and condition.value.kind.endswith("List"):
                raise ValueError("scalar comparator needs one value")
            if operator in (">", ">=", "<", "<=") and condition.value.kind not in (
                "number",
                "date",
                "dateTime",
            ):
                raise ValueError("ordered comparison needs number or time")
            criterion = condition.field + " " + operator + " " + _condition_value(condition.value)
        rendered = criterion if not rendered else "(" + rendered + " and " + criterion + ")"
    sort = input.sort
    if isinstance(sort, dict):
        sort = RecordSort(sort.get("field", ""), sort.get("isDescending", False))
    if sort is None or sort.field == "id":
        order = "id " + ("desc" if sort is not None and sort.is_descending else "asc")
    else:
        if not _FIELD_PATH.fullmatch(sort.field):
            raise ValueError("invalid sort field")
        order = sort.field + (" desc" if sort.is_descending else " asc") + ", id asc"
    selected = ", ".join(field for field in input.fields if field != "id") or "id"
    return (
        f"select {selected} from {input.module} where {rendered} "
        f"order by {order} limit {input.offset}, {limit}"
    )


class FindRecordsOperation:
    """COQL read of records matching typed conditions."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return FIND_RECORDS_DEFINITION

    async def invoke(self, call: Call, input: FindRecordsInput) -> QueryAttempt[FindRecordsOutput]:
        """Read one result page and its next offset."""
        try:
            query = build_find_records_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                FindRecordsOutput(),
                Receipt(),
                failure("findRecords", FailureKind.VALIDATION, str(error)),
            )
        exchange = await self.client.exchange(
            call, "findRecords", "POST", "/coql", payload={"select_query": query}
        )
        terminal = _query_result(exchange, call, "findRecords", FindRecordsOutput())
        if terminal:
            return terminal
        try:
            records, has_more = _records(input.module, exchange.body)
            if len(records) > (input.limit or 10):
                raise ValueError("Zoho CRM returned more records than the page size")
        except (ValueError, TypeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                FindRecordsOutput(),
                call_receipt(call, exchange),
                failure("findRecords", FailureKind.PROTOCOL, "Zoho CRM returned an invalid page"),
            )
        output = FindRecordsOutput(
            input.module,
            records,
            input.offset + len(records)
            if has_more and records and input.offset + len(records) < 100000
            else 0,
        )
        return QueryAttempt.branch_result(
            "found" if records else "notFound", output, call_receipt(call, exchange)
        )


class GetRecordOperation:
    """Read one Zoho CRM record by numeric ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return GET_RECORD_DEFINITION

    async def invoke(self, call: Call, input: GetRecordInput) -> QueryAttempt[Record]:
        """Read one record and reject a mismatched provider identity."""
        try:
            _module(input.module)
            _record_id(input.record_id)
            _fields(input.fields, 0, path=False)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                Record(fields={}),
                Receipt(),
                failure("getRecord", FailureKind.VALIDATION, str(error)),
            )
        query = (
            urlencode({"fields": ",".join(input.fields)}, quote_via=quote, safe="~") if input.fields else ""
        )
        exchange = await self.client.exchange(
            call,
            "getRecord",
            "GET",
            f"/{quote(input.module, safe='~')}/{quote(input.record_id, safe='~')}",
            query=query,
        )
        terminal = _query_result(exchange, call, "getRecord", Record(fields={}), not_found=True)
        if terminal:
            return terminal
        if exchange.status == 204 or not exchange.body.strip():
            return QueryAttempt.branch_result(
                "notFound",
                Record(fields={}),
                call_receipt(call, exchange),
                failure(
                    "getRecord", FailureKind.NOT_FOUND, "Zoho CRM found no record with the ID (HTTP 204)"
                ),
            )
        try:
            records, _ = _records(input.module, exchange.body)
        except (ValueError, TypeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                Record(fields={}),
                call_receipt(call, exchange),
                failure("getRecord", FailureKind.PROTOCOL, "Zoho CRM returned an invalid record"),
            )
        if not records:
            return QueryAttempt.branch_result(
                "notFound",
                Record(fields={}),
                call_receipt(call, exchange),
                failure("getRecord", FailureKind.NOT_FOUND, "Zoho CRM returned no record with the ID"),
            )
        if len(records) != 1 or records[0].id != input.record_id:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Record(fields={}),
                call_receipt(call, exchange),
                failure(
                    "getRecord",
                    FailureKind.PROTOCOL,
                    "Zoho CRM returned a record other than the one requested",
                ),
            )
        return QueryAttempt.branch_result("found", records[0], call_receipt(call, exchange, input.record_id))


def build_list_modified_records_query(input: ListModifiedRecordsInput) -> tuple[str, str]:
    """Build watermark COQL and the cursor used if the page is empty."""
    _module(input.module)
    maximum_fields = 50 if "Modified_Time" in input.fields else 49
    if not input.fields or len(input.fields) > maximum_fields:
        raise ValueError(f"fields must name 1 to {maximum_fields} field API names")
    fields = tuple(input.fields) if "Modified_Time" in input.fields else (*input.fields, "Modified_Time")
    _fields(fields, 1)
    if type(input.limit) is not int:
        raise ValueError("limit must be an integer")
    limit = input.limit or 100
    if type(limit) is not int or not 1 <= limit <= 200:
        raise ValueError("limit must be 1 to 200")
    if input.cursor:
        if input.modified_since != "0001-01-01T00:00:00Z":
            raise ValueError("set modifiedSince or cursor, not both")
        match = _CURSOR.fullmatch(input.cursor)
        if not match:
            raise ValueError("cursor must be a cursor returned by listModifiedRecords")
        timestamp = _zoho_time(match.group(1)).strftime("%Y-%m-%dT%H:%M:%SZ")
        record_id = match.group(2).lstrip("0") or "0"
    else:
        if input.modified_since == "0001-01-01T00:00:00Z":
            raise ValueError("modifiedSince or cursor is required")
        timestamp = _zoho_time(input.modified_since).strftime("%Y-%m-%dT%H:%M:%SZ")
        record_id = "0"
    zoho_timestamp = timestamp[:-1] + "+00:00"
    condition = (
        f"Modified_Time >= '{zoho_timestamp}'"
        if record_id == "0"
        else (
            f"(Modified_Time > '{zoho_timestamp}' or "
            f"(Modified_Time = '{zoho_timestamp}' and id > {record_id}))"
        )
    )
    selected = ", ".join(field for field in fields if field != "id") or "id"
    query = (
        f"select {selected} from {input.module} where {condition} "
        f"order by Modified_Time asc, id asc limit 0, {limit}"
    )
    return query, timestamp + "/" + record_id


class ListModifiedRecordsOperation:
    """Read changed records with an ordered watermark."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return LIST_MODIFIED_RECORDS_DEFINITION

    async def invoke(
        self, call: Call, input: ListModifiedRecordsInput
    ) -> QueryAttempt[ListModifiedRecordsOutput]:
        """Read a page while rejecting missing or unordered modification times."""
        try:
            query, cursor = build_list_modified_records_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                ListModifiedRecordsOutput(),
                Receipt(),
                failure("listModifiedRecords", FailureKind.VALIDATION, str(error)),
            )
        exchange = await self.client.exchange(
            call, "listModifiedRecords", "POST", "/coql", payload={"select_query": query}
        )
        terminal = _query_result(exchange, call, "listModifiedRecords", ListModifiedRecordsOutput())
        if terminal:
            return terminal
        try:
            records, has_more = _records(input.module, exchange.body)
            if len(records) > (input.limit or 100):
                raise ValueError("Zoho CRM returned more records than the page size")
            previous_time, previous_id = cursor.split("/", 1)
            for record in records:
                text = record.fields.get("Modified_Time") if record.fields else None
                timestamp = _zoho_time(str(text)).strftime("%Y-%m-%dT%H:%M:%SZ")
                if timestamp < previous_time or (
                    timestamp == previous_time and not _decimal_id_after(record.id, previous_id)
                ):
                    raise ValueError(
                        "records are not in ascending Modified_Time and id order after the cursor"
                    )
                previous_time, previous_id = timestamp, record.id.lstrip("0") or "0"
            cursor = previous_time + "/" + previous_id
        except (ValueError, TypeError, AttributeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListModifiedRecordsOutput(),
                call_receipt(call, exchange),
                failure("listModifiedRecords", FailureKind.PROTOCOL, "Zoho CRM returned an invalid page"),
            )
        output = ListModifiedRecordsOutput(input.module, records, bool(has_more and records), cursor)
        return QueryAttempt.branch_result("listed", output, call_receipt(call, exchange))


def _validate_write_fields(values: dict[str, Any] | None) -> None:
    if not isinstance(values, dict) or not 1 <= len(values) <= 200:
        raise ValueError("fields must set 1 to 200 fields")
    for name, value in values.items():
        if not isinstance(name, str) or not _FIELD.fullmatch(name) or name.lower() == "id":
            raise ValueError("field name must be a Zoho CRM field API name other than id")
        try:
            json.dumps(value, allow_nan=False)
        except (TypeError, ValueError):
            raise ValueError("field must hold one JSON value") from None


def _trigger(values: tuple[str, ...], should_skip_automation: bool) -> list[str] | None:
    if should_skip_automation:
        if values:
            raise ValueError("set triggers or shouldSkipAutomation, not both")
        return []
    if not values:
        return None
    if len(set(values)) != len(values) or any(
        value not in ("workflow", "approval", "blueprint") for value in values
    ):
        raise ValueError("triggers must be distinct workflow, approval, or blueprint names")
    return list(values)


def _write_result(exchange: Exchange) -> tuple[str, str, str, str, str]:
    document = _strict_json(exchange.body)
    if (
        not isinstance(document, dict)
        or not isinstance(document.get("data"), list)
        or len(document["data"]) != 1
    ):
        raise ValueError("the response holds no single-record result")
    result = document["data"][0]
    if not isinstance(result, dict) or result.get("code") != "SUCCESS":
        raise ValueError("the result is not a SUCCESS entry")
    details = result.get("details")
    if (
        not isinstance(details, dict)
        or not isinstance(details.get("id"), str)
        or not _RECORD_ID.fullmatch(details["id"])
    ):
        raise ValueError("the result has no numeric record id")
    duplicate_field = result.get("duplicate_field")
    duplicate_field = (
        duplicate_field if isinstance(duplicate_field, str) and _FIELD.fullmatch(duplicate_field) else ""
    )

    def optional_time(name: str) -> str:
        value = details.get(name)
        if not isinstance(value, str):
            return "0001-01-01T00:00:00Z"
        try:
            _zoho_time(value)
            timestamp = re.fullmatch(
                r"([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2})(?:\.([0-9]+))?(Z|[+-][0-9]{2}:[0-9]{2})",
                value,
            )
            assert timestamp is not None
            fraction = (timestamp.group(2) or "")[:9].rstrip("0")
            suffix = "Z" if timestamp.group(3) in ("+00:00", "-00:00") else timestamp.group(3)
            return timestamp.group(1) + ("." + fraction if fraction else "") + suffix
        except ValueError:
            return "0001-01-01T00:00:00Z"

    return (
        details["id"],
        result.get("action", ""),
        duplicate_field,
        optional_time("Created_Time"),
        optional_time("Modified_Time"),
    )


class UpsertRecordOperation:
    """Repeatable create-or-update with a Zoho duplicate-check field."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return UPSERT_RECORD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertRecordInput) -> IdempotencyKey:
        """Use the stable Dex call ID for receipt correlation."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpsertRecordInput) -> MutationAttempt[UpsertRecordOutput]:
        """Upsert one record, resending one duplicate-data conflict."""
        try:
            _module(input.module)
            _validate_write_fields(input.fields)
            if not 1 <= len(input.duplicate_check_fields) <= 3 or len(
                set(input.duplicate_check_fields)
            ) != len(input.duplicate_check_fields):
                raise ValueError("duplicateCheckFields must name 1 to 3 distinct fields")
            assert input.fields is not None
            for name in input.duplicate_check_fields:
                if (
                    not _FIELD.fullmatch(name)
                    or name not in input.fields
                    or input.fields[name] in (None, "", [], {})
                    or isinstance(input.fields[name], str) and not input.fields[name].strip()
                ):
                    raise ValueError("fields must set each duplicate-check field to a non-empty value")
            trigger = _trigger(input.triggers, input.should_skip_automation)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                UpsertRecordOutput(),
                Receipt(),
                failure("upsertRecord", FailureKind.VALIDATION, str(error)),
            )
        payload: dict[str, Any] = {
            "data": [input.fields],
            "duplicate_check_fields": list(input.duplicate_check_fields),
        }
        if trigger is not None:
            payload["trigger"] = trigger
        deadline_at = asyncio.get_running_loop().time() + 25
        for send in range(2):
            exchange = await self.client.exchange(
                call,
                "upsertRecord",
                "POST",
                f"/{quote(input.module, safe='~')}/upsert",
                payload=payload,
                deadline_at=deadline_at,
            )
            exchange = self.client.classify_write_exchange("upsertRecord", exchange)
            if exchange.outcome == "succeeded":
                try:
                    written = _write_result(exchange)
                except (ValueError, TypeError, UnicodeDecodeError):
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        UpsertRecordOutput(input.module),
                        call_receipt(call, exchange),
                        failure(
                            "upsertRecord", FailureKind.PROTOCOL, "Zoho CRM returned an invalid write result"
                        ),
                    )
                return MutationAttempt.branch_result(
                    "upserted",
                    UpsertRecordOutput(
                        input.module, written[0], written[1] == "insert", written[2], written[3], written[4]
                    ),
                    call_receipt(call, exchange, written[0]),
                )
            if exchange.outcome == "rejected" and exchange.summary.code == "DUPLICATE_DATA" and send == 0:
                continue
            rejected = UpsertRecordOutput(input.module, provider_errors=exchange.summary.provider_errors())
            terminal = _mutation_result(exchange, call, "upsertRecord", rejected)
            if terminal:
                return terminal
        raise AssertionError("unreachable upsert result")


class UpdateRecordOperation:
    """Repeatable absolute field update for one record."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return UPDATE_RECORD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateRecordInput) -> IdempotencyKey:
        """Use the stable Dex call ID for receipt correlation."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateRecordInput) -> MutationAttempt[UpdateRecordOutput]:
        """Set named fields while preserving all others."""
        try:
            _module(input.module)
            _record_id(input.record_id)
            _validate_write_fields(input.fields)
            trigger = _trigger(input.triggers, input.should_skip_automation)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                UpdateRecordOutput(),
                Receipt(),
                failure("updateRecord", FailureKind.VALIDATION, str(error)),
            )
        payload: dict[str, Any] = {"data": [input.fields]}
        if trigger is not None:
            payload["trigger"] = trigger
        path = f"/{quote(input.module, safe='~')}/{quote(input.record_id, safe='~')}"
        exchange = await self.client.exchange(call, "updateRecord", "PUT", path, payload=payload)
        exchange = self.client.classify_write_exchange("updateRecord", exchange)
        if exchange.outcome == "succeeded":
            try:
                written = _write_result(exchange)
            except (ValueError, TypeError, UnicodeDecodeError):
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    UpdateRecordOutput(input.module),
                    call_receipt(call, exchange, input.record_id),
                    failure(
                        "updateRecord", FailureKind.PROTOCOL, "Zoho CRM returned an invalid write result"
                    ),
                )
            if written[0] != input.record_id:
                return MutationAttempt.branch_result(
                    "invalidResponse",
                    UpdateRecordOutput(input.module),
                    call_receipt(call, exchange, input.record_id),
                    failure(
                        "updateRecord",
                        FailureKind.PROTOCOL,
                        "Zoho CRM reported a write to a record other than the one requested",
                    ),
                )
            return MutationAttempt.branch_result(
                "updated",
                UpdateRecordOutput(input.module, written[0], written[4]),
                call_receipt(call, exchange, input.record_id),
            )
        rejected = UpdateRecordOutput(input.module, provider_errors=exchange.summary.provider_errors())
        terminal = _mutation_result(exchange, call, "updateRecord", rejected, not_found=True)
        if terminal:
            return terminal
        raise AssertionError("unreachable update result")


class ListModuleFieldsOperation:
    """Read field metadata and picklist values for one module."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> Any:
        """Return the manifest definition."""
        return LIST_MODULE_FIELDS_DEFINITION

    async def invoke(self, call: Call, input: ListModuleFieldsInput) -> QueryAttempt[ListModuleFieldsOutput]:
        """Decode one bounded field metadata response."""
        try:
            _module(input.module)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                ListModuleFieldsOutput(),
                Receipt(),
                failure("listModuleFields", FailureKind.VALIDATION, str(error)),
            )
        query = urlencode({"module": input.module})
        exchange = await self.client.exchange(
            call, "listModuleFields", "GET", "/settings/fields", query=query
        )
        terminal = _query_result(exchange, call, "listModuleFields", ListModuleFieldsOutput())
        if terminal:
            return terminal
        try:
            document = _strict_json(exchange.body)
            if not isinstance(document, dict) or not isinstance(document.get("fields"), list):
                raise ValueError("the response is not a JSON object with a fields list")
            fields = []
            for item in document["fields"]:
                if (
                    not isinstance(item, dict)
                    or not isinstance(item.get("api_name"), str)
                    or not _FIELD.fullmatch(item["api_name"])
                    or not isinstance(item.get("data_type"), str)
                    or not item["data_type"]
                ):
                    raise ValueError("a field has no valid api_name or data_type")
                lookup = item.get("lookup")
                lookup_module = lookup.get("module") if isinstance(lookup, dict) else None
                lookup_name = lookup_module.get("api_name") if isinstance(lookup_module, dict) else ""
                values = item.get("pick_list_values") or []
                if not isinstance(values, list):
                    raise ValueError("invalid picklist values")
                picklist = tuple(
                    PicklistValue(
                        value.get("actual_value", ""),
                        value.get("display_value", ""),
                        value.get("type") == "unused",
                    )
                    for value in values
                    if isinstance(value, dict)
                )
                unique = item.get("unique")
                fields.append(
                    ModuleField(
                        item["api_name"],
                        item.get("display_label", ""),
                        item["data_type"],
                        item.get("custom_field", False),
                        item.get("read_only", False),
                        item.get("system_mandatory", False),
                        isinstance(unique, dict) and bool(unique),
                        max(item.get("length", 0), 0),
                        lookup_name if isinstance(lookup_name, str) and _FIELD.fullmatch(lookup_name) else "",
                        picklist,
                    )
                )
        except (ValueError, TypeError, UnicodeDecodeError, AttributeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListModuleFieldsOutput(),
                call_receipt(call, exchange),
                failure("listModuleFields", FailureKind.PROTOCOL, "Zoho CRM returned an invalid field list"),
            )
        return QueryAttempt.branch_result(
            "listed", ListModuleFieldsOutput(input.module, tuple(fields)), call_receipt(call, exchange)
        )
