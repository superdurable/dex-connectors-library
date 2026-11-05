# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho CRM operation invocations and stable branch routing."""

import json
import re
import time
from datetime import UTC, datetime
from typing import Any, TypeVar

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
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
from .client import Client, Exchange, classify_rejection, describe_error, failure, path_escape, query_escape
from .coql import (
    FIELD,
    find_records_query,
    select_query,
    validate_fields,
    validate_module,
    validate_record_id,
)
from .models import (
    FindRecordsInput,
    FindRecordsOutput,
    GetRecordInput,
    ListModifiedRecordsInput,
    ListModifiedRecordsOutput,
    ListModuleFieldsInput,
    ListModuleFieldsOutput,
    Record,
    UpdateRecordInput,
    UpdateRecordOutput,
    UpsertRecordInput,
    UpsertRecordOutput,
)
from .records import decode_module_fields, decode_page, decode_write, provider_errors

Output = TypeVar("Output")


def _query_terminal(
    operation: str, exchange: Exchange, receipt: Receipt, empty: Output, *, not_found: bool = False
) -> QueryAttempt[Output] | None:
    if exchange.outcome == "success":
        return None
    if exchange.outcome == "retry":
        assert exchange.failure is not None
        return QueryAttempt.retry(exchange.failure, exchange.retry_after)
    branch = {
        "invalid": "invalidResponse",
        "defect": "defect",
    }.get(
        exchange.outcome,
        "notFound" if not_found and exchange.summary.record_id_invalid() else "providerRejected",
    )
    return QueryAttempt.branch_result(branch, empty, receipt, exchange.failure)


def _mutation_terminal(
    operation: str, exchange: Exchange, receipt: Receipt, empty: Output, *, has_not_found: bool = False
) -> MutationAttempt[Output] | None:
    if exchange.outcome == "success":
        return None
    if exchange.outcome == "retry":
        assert exchange.failure is not None
        return MutationAttempt.retry(exchange.failure, exchange.retry_after)
    if exchange.outcome == "invalid":
        branch = "invalidResponse"
    elif exchange.outcome == "defect":
        branch = "defect"
    elif exchange.summary.code == "DUPLICATE_DATA":
        branch = "conflict"
    elif has_not_found and exchange.summary.record_id_invalid():
        branch = "notFound"
    elif exchange.summary.code in {
        "OAUTH_SCOPE_MISMATCH",
        "NO_PERMISSION",
        "AUTHORIZATION_FAILED",
        "INVALID_MODULE",
        "NOT_SUPPORTED",
        "INVALID_URL_PATTERN",
        "INVALID_REQUEST_METHOD",
    } or (exchange.status >= 300 and exchange.status not in (400, 422)):
        branch = "providerRejected"
    else:
        branch = "recordRejected"
    return MutationAttempt.branch_result(branch, empty, receipt, exchange.failure)


def _validation_query(operation: str, empty: Output, message: str) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, message)
    )


def _validation_mutation(operation: str, empty: Output, message: str) -> MutationAttempt[Output]:
    return MutationAttempt.branch_result(
        "defect", empty, Receipt(), failure(FailureKind.VALIDATION, operation, message)
    )


class FindRecordsOperation:
    """Find committed records with a typed COQL predicate."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable operation definition."""
        return FIND_RECORDS_DEFINITION

    async def invoke(self, call: Call, input: FindRecordsInput) -> QueryAttempt[FindRecordsOutput]:
        """Send COQL, validate the page, and choose the matching branch."""
        operation = "findRecords"
        try:
            query = find_records_query(input)
        except ValueError as error:
            return _validation_query(operation, FindRecordsOutput(), str(error))
        exchange = await self.client.exchange(
            call, operation, "POST", "/coql", payload={"select_query": query}
        )
        receipt = self.client.receipt(call, exchange)
        terminal = _query_terminal(operation, exchange, receipt, FindRecordsOutput())
        if terminal is not None:
            return terminal
        try:
            records, has_more = decode_page(input.module, exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                FindRecordsOutput(),
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Zoho CRM returned an invalid page: " + str(error)),
            )
        if len(records) > (input.limit or 10):
            return QueryAttempt.branch_result(
                "invalidResponse",
                FindRecordsOutput(),
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Zoho CRM returned more records than the page size"),
            )
        next_offset = (
            input.offset + len(records)
            if has_more and records and input.offset + len(records) < 100000
            else 0
        )
        output = FindRecordsOutput(input.module, records, next_offset)
        return QueryAttempt.branch_result("found" if records else "notFound", output, receipt)


class GetRecordOperation:
    """Read one Zoho CRM record by its decimal ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable operation definition."""
        return GET_RECORD_DEFINITION

    async def invoke(self, call: Call, input: GetRecordInput) -> QueryAttempt[Record]:
        """Read the record and reject mismatched provider identity."""
        operation = "getRecord"
        try:
            validate_module(input.module)
            validate_record_id(input.record_id)
            validate_fields(input.fields, 0)
            if any(not FIELD.fullmatch(name) for name in input.fields):
                raise ValueError(
                    "getRecord fields must be field API names without a lookup path; use findRecords for Account_Name.Account_Name"
                )
        except ValueError as error:
            return _validation_query(operation, Record(fields={}), str(error))
        query = "fields=" + query_escape(",".join(input.fields)) if input.fields else ""
        exchange = await self.client.exchange(
            call,
            operation,
            "GET",
            "/" + path_escape(input.module) + "/" + path_escape(input.record_id),
            query=query,
        )
        receipt = self.client.receipt(call, exchange, input.record_id)
        terminal = _query_terminal(operation, exchange, receipt, Record(fields={}), not_found=True)
        if terminal is not None:
            return terminal
        if exchange.status == 204 or not exchange.body.strip():
            return QueryAttempt.branch_result(
                "notFound",
                Record(fields={}),
                receipt,
                failure(FailureKind.NOT_FOUND, operation, "Zoho CRM found no record with the ID (HTTP 204)"),
            )
        try:
            records, _ = decode_page(input.module, exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Record(fields={}),
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Zoho CRM returned an invalid record: " + str(error)
                ),
            )
        if not records:
            return QueryAttempt.branch_result(
                "notFound",
                Record(fields={}),
                receipt,
                failure(FailureKind.NOT_FOUND, operation, "Zoho CRM returned no record with the ID"),
            )
        if len(records) != 1 or records[0].id != input.record_id:
            return QueryAttempt.branch_result(
                "invalidResponse",
                Record(fields={}),
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Zoho CRM returned a record other than the one requested"
                ),
            )
        return QueryAttempt.branch_result("found", records[0], receipt)


class ListModuleFieldsOperation:
    """Read module field and picklist metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable operation definition."""
        return LIST_MODULE_FIELDS_DEFINITION

    async def invoke(self, call: Call, input: ListModuleFieldsInput) -> QueryAttempt[ListModuleFieldsOutput]:
        """Read field metadata with the selected module name."""
        operation = "listModuleFields"
        try:
            validate_module(input.module)
        except ValueError as error:
            return _validation_query(operation, ListModuleFieldsOutput(), str(error))
        exchange = await self.client.exchange(
            call, operation, "GET", "/settings/fields", query="module=" + query_escape(input.module)
        )
        receipt = self.client.receipt(call, exchange)
        terminal = _query_terminal(operation, exchange, receipt, ListModuleFieldsOutput())
        if terminal is not None:
            return terminal
        try:
            fields = decode_module_fields(exchange.body)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListModuleFieldsOutput(),
                receipt,
                failure(
                    FailureKind.PROTOCOL, operation, "Zoho CRM returned an invalid field list: " + str(error)
                ),
            )
        return QueryAttempt.branch_result("listed", ListModuleFieldsOutput(input.module, fields), receipt)


def _render_trigger(triggers: tuple[str, ...], should_skip: bool) -> list[str] | None:
    if should_skip:
        if triggers:
            raise ValueError("set triggers or shouldSkipAutomation, not both")
        return []
    if not triggers:
        return None
    if len(set(triggers)) != len(triggers):
        raise ValueError("triggers must not repeat")
    if any(trigger not in ("workflow", "approval", "blueprint") for trigger in triggers):
        raise ValueError("each trigger must be workflow, approval, or blueprint")
    return list(triggers)


def _validate_write_fields(fields: dict[str, Any] | None) -> dict[str, Any]:
    if fields is None or not 1 <= len(fields) <= 200:
        raise ValueError("fields must set 1 to 200 fields")
    for name, value in fields.items():
        if not FIELD.fullmatch(name) or name.lower() == "id":
            raise ValueError(f'field name "{name}" must be a Zoho CRM field API name other than id')
        try:
            json.dumps(value, allow_nan=False)
        except (TypeError, ValueError) as error:
            raise ValueError(f"field {name} must hold one JSON value") from error
    return fields


def _write_exchange(
    exchange: Exchange, operation: str
) -> tuple[Exchange, tuple[str, str, str, str, str]]:
    if exchange.outcome != "success":
        return exchange, ("", "", "", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z")
    try:
        action, duplicate_field, record_id, created_at, modified_at, error_item = decode_write(exchange.body)
    except ValueError as error:
        return Exchange(
            "invalid",
            exchange.status,
            exchange.headers,
            failure=failure(
                FailureKind.PROTOCOL, operation, "Zoho CRM returned an invalid write result: " + str(error)
            ),
            remaining_credits=exchange.remaining_credits,
        ), ("", "", "", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z")
    if error_item is not None:
        summary = describe_error(json.dumps({"data": [error_item]}).encode())
        rejected = classify_rejection(
            exchange.status, exchange.headers, summary, operation, exchange.remaining_credits
        )
        return rejected, ("", "", "", "0001-01-01T00:00:00Z", "0001-01-01T00:00:00Z")
    return exchange, (action, duplicate_field, record_id, created_at, modified_at)


class UpsertRecordOperation:
    """Create or update a record using provider duplicate-check fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable operation definition."""
        return UPSERT_RECORD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertRecordInput) -> IdempotencyKey:
        """Correlate repeated upserts with the stable Dex Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpsertRecordInput) -> MutationAttempt[UpsertRecordOutput]:
        """Send a repeatable upsert and resend one duplicate rejection."""
        operation = "upsertRecord"
        try:
            validate_module(input.module)
            fields = _validate_write_fields(input.fields)
            duplicate_fields = input.duplicate_check_fields
            if not 1 <= len(duplicate_fields) <= 3:
                raise ValueError("duplicateCheckFields must name 1 to 3 fields")
            for index, name in enumerate(duplicate_fields):
                if not FIELD.fullmatch(name) or name in duplicate_fields[:index]:
                    raise ValueError(
                        "duplicateCheckFields must be distinct Zoho CRM field API names such as Email"
                    )
                if name not in fields or fields[name] in (None, "", [], {}):
                    raise ValueError(
                        f"fields must set duplicate-check field {name} to a non-empty value, or every attempt would insert a new record"
                    )
            trigger = _render_trigger(input.triggers, input.should_skip_automation)
        except ValueError as error:
            return _validation_mutation(operation, UpsertRecordOutput(), str(error))
        body: dict[str, Any] = {"data": [fields], "duplicate_check_fields": list(duplicate_fields)}
        if trigger is not None:
            body["trigger"] = trigger
        deadline = time.monotonic() + 25
        for send in range(2):
            exchange = await self.client.exchange(
                call, operation, "POST", "/" + path_escape(input.module) + "/upsert", payload=body,
                deadline=deadline,
            )
            exchange, written = _write_exchange(exchange, operation)
            if exchange.outcome == "rejected" and exchange.summary.code == "DUPLICATE_DATA" and send == 0:
                continue
            receipt = self.client.receipt(call, exchange, written[2])
            rejected = UpsertRecordOutput(
                module=input.module,
                provider_errors=provider_errors(
                    exchange.summary.code, exchange.summary.field, exchange.summary.duplicate_record_id
                ),
            )
            terminal = _mutation_terminal(operation, exchange, receipt, rejected)
            if terminal is not None:
                return terminal
            action, duplicate_field, record_id, created_at, modified_at = written
            return MutationAttempt.branch_result(
                "upserted",
                UpsertRecordOutput(
                    module=input.module,
                    id=record_id,
                    is_created=action == "insert",
                    duplicate_field=duplicate_field,
                    created_at=created_at,
                    modified_at=modified_at,
                ),
                receipt,
            )
        raise RuntimeError("Zoho upsert send budget exhausted")


class UpdateRecordOperation:
    """Set absolute values on one record with repeatable PUT semantics."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the stable operation definition."""
        return UPDATE_RECORD_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateRecordInput) -> IdempotencyKey:
        """Correlate repeated updates with the stable Dex Call ID."""
        return IdempotencyKey(call_id)

    async def invoke(self, call: Call, input: UpdateRecordInput) -> MutationAttempt[UpdateRecordOutput]:
        """Update one record and validate the returned identity."""
        operation = "updateRecord"
        try:
            validate_module(input.module)
            validate_record_id(input.record_id)
            fields = _validate_write_fields(input.fields)
            trigger = _render_trigger(input.triggers, input.should_skip_automation)
        except ValueError as error:
            return _validation_mutation(operation, UpdateRecordOutput(), str(error))
        body: dict[str, Any] = {"data": [fields]}
        if trigger is not None:
            body["trigger"] = trigger
        exchange = await self.client.exchange(
            call,
            operation,
            "PUT",
            "/" + path_escape(input.module) + "/" + path_escape(input.record_id),
            payload=body,
        )
        exchange, written = _write_exchange(exchange, operation)
        receipt = self.client.receipt(call, exchange, input.record_id)
        rejected = UpdateRecordOutput(
            module=input.module,
            provider_errors=provider_errors(
                exchange.summary.code, exchange.summary.field, exchange.summary.duplicate_record_id
            ),
        )
        terminal = _mutation_terminal(operation, exchange, receipt, rejected, has_not_found=True)
        if terminal is not None:
            return terminal
        if written[2] != input.record_id:
            return MutationAttempt.branch_result(
                "invalidResponse",
                UpdateRecordOutput(module=input.module),
                receipt,
                failure(
                    FailureKind.PROTOCOL,
                    operation,
                    "Zoho CRM reported a write to a record other than the one requested",
                ),
            )
        return MutationAttempt.branch_result(
            "updated", UpdateRecordOutput(module=input.module, id=written[2], modified_at=written[4]), receipt
        )


_CURSOR = re.compile(r"([0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}:[0-9]{2}:[0-9]{2}Z)/([0-9]{1,20})\Z")


def _parse_cursor(cursor: str) -> tuple[datetime, str]:
    match = _CURSOR.fullmatch(cursor)
    if match is None:
        raise ValueError("cursor must be a cursor returned by listModifiedRecords")
    try:
        timestamp = datetime.fromisoformat(match[1].replace("Z", "+00:00"))
    except ValueError as error:
        raise ValueError("cursor must be a cursor returned by listModifiedRecords") from error
    return timestamp, match[2].lstrip("0")


def _cursor(timestamp: datetime, record_id: str) -> str:
    return timestamp.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ") + "/" + (record_id or "0")


def _modified_query(input: ListModifiedRecordsInput) -> tuple[str, datetime, str]:
    validate_module(input.module)
    fields = input.fields + (() if "Modified_Time" in input.fields else ("Modified_Time",))
    if not input.fields:
        raise ValueError("fields must name 1 to 49 field API names")
    validate_fields(fields, 1)
    limit = input.limit or 100
    if not 1 <= limit <= 200:
        raise ValueError("limit must be 1 to 200")
    has_modified_since = input.modified_since != "0001-01-01T00:00:00Z"
    if input.cursor and has_modified_since:
        raise ValueError("set modifiedSince to start a feed or cursor to continue one, not both")
    if input.cursor:
        modified_at, record_id = _parse_cursor(input.cursor)
    elif has_modified_since:
        modified_at = datetime.fromisoformat(input.modified_since.replace("Z", "+00:00"))
        modified_at = modified_at.astimezone(UTC).replace(microsecond=0)
        record_id = "0"
    else:
        raise ValueError("modifiedSince or cursor is required")
    instant = modified_at.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S+00:00")
    if record_id in ("", "0"):
        where = f"Modified_Time >= '{instant}'"
    else:
        where = f"(Modified_Time > '{instant}' or (Modified_Time = '{instant}' and id > {record_id}))"
    return (
        select_query(input.module, fields, where, "Modified_Time asc, id asc", 0, limit),
        modified_at,
        record_id,
    )


class ListModifiedRecordsOperation:
    """Page changed records by a durable modification watermark."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable operation definition."""
        return LIST_MODIFIED_RECORDS_DEFINITION

    async def invoke(
        self, call: Call, input: ListModifiedRecordsInput
    ) -> QueryAttempt[ListModifiedRecordsOutput]:
        """Read records strictly after the supplied cursor."""
        operation = "listModifiedRecords"
        try:
            query, modified_at, record_id = _modified_query(input)
        except ValueError as error:
            return _validation_query(operation, ListModifiedRecordsOutput(), str(error))
        exchange = await self.client.exchange(
            call, operation, "POST", "/coql", payload={"select_query": query}
        )
        receipt = self.client.receipt(call, exchange)
        terminal = _query_terminal(operation, exchange, receipt, ListModifiedRecordsOutput())
        if terminal is not None:
            return terminal
        try:
            records, has_more = decode_page(input.module, exchange.body)
            if len(records) > (input.limit or 100):
                raise ValueError("Zoho CRM returned more records than the page size")
            for record in records:
                value = record.fields.get("Modified_Time") if record.fields else None
                if not isinstance(value, str):
                    raise ValueError("a record has no valid Modified_Time")
                try:
                    next_time = datetime.fromisoformat(value.replace("Z", "+00:00"))
                except ValueError as error:
                    raise ValueError("a record has no valid Modified_Time") from error
                next_time = next_time.astimezone(UTC).replace(microsecond=0)
                next_id = record.id.lstrip("0")
                if next_time < modified_at or (
                    next_time == modified_at
                    and record_id not in ("", "0")
                    and (len(next_id), next_id) <= (len(record_id), record_id)
                ):
                    raise ValueError(
                        "records are not in ascending Modified_Time and id order after the cursor"
                    )
                modified_at, record_id = next_time, next_id
        except ValueError as error:
            return QueryAttempt.branch_result(
                "invalidResponse",
                ListModifiedRecordsOutput(),
                receipt,
                failure(FailureKind.PROTOCOL, operation, "Zoho CRM returned an invalid page: " + str(error)),
            )
        output = ListModifiedRecordsOutput(
            module=input.module,
            records=records,
            has_more=has_more and bool(records),
            cursor=_cursor(modified_at, record_id),
        )
        return QueryAttempt.branch_result("listed", output, receipt)
