# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Airtable record operations with Go-compatible pacing, classification, and decoding.

Airtable has no idempotency key. upsertRecords is repeat-safe because a retried write finds
the record the first write created, and updateRecords sets absolute values. Requests to one
base are spaced to Airtable's limit of 5 requests per second within one process, and a 429
holds that base back for Airtable's documented 30 seconds.
"""

import asyncio
import json
import math
import re
import time
import unicodedata
from dataclasses import dataclass
from datetime import UTC, datetime
from decimal import Decimal
from enum import Enum
from typing import Any, TypeVar
from urllib.parse import quote

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_RECORD_DEFINITION,
    GET_RECORD_OPERATION_ID,
    INVALID_RESPONSE,
    LIST_RECORDS_DEFINITION,
    LIST_RECORDS_OPERATION_ID,
    LISTED,
    NOT_FOUND,
    OFFSET_EXPIRED,
    PROVIDER_REJECTED,
    UPDATE_RECORDS_DEFINITION,
    UPDATE_RECORDS_OPERATION_ID,
    UPDATED,
    UPSERT_RECORDS_DEFINITION,
    UPSERT_RECORDS_OPERATION_ID,
    UPSERTED,
)
from .models import (
    FieldEqualityFilter,
    GetRecordInput,
    ListRecordsInput,
    Record,
    RecordPage,
    RecordSort,
    RecordUpdate,
    RecordUpsert,
    UpdatedRecords,
    UpdateRecordsInput,
    UpsertedRecords,
    UpsertRecordsInput,
)

UI_UNIT_BASE_PICKER = "basePicker"
UI_BASE_PICKER_PORT_BASE_ID = "baseId"
UI_BASE_PICKER_PORT_BASE_NAME = "baseName"
UI_UNIT_TABLE_PICKER = "tablePicker"
UI_TABLE_PICKER_PORT_BASE_ID = "baseId"
UI_TABLE_PICKER_PORT_TABLE_ID = "tableId"
UI_TABLE_PICKER_PORT_TABLE_NAME = "tableName"
MAXIMUM_PAGE_SIZE = 100
MAXIMUM_RECORDS_PER_WRITE = 10
MAXIMUM_FIELDS_PER_RECORD = 500
MAXIMUM_MERGE_FIELDS = 3
MAXIMUM_FIELD_EQUALITY_FILTERS = 25
MAXIMUM_FORMULA_BYTES = 16000
MAXIMUM_LISTED_FIELDS = 500
MAXIMUM_RECORD_SORTS = 10
_DEFAULT_ENDPOINT = "https://api.airtable.com"
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
_REQUEST_TIMEOUT_SECONDS = 25
_MAXIMUM_OFFSET_BYTES = 1024
_MAXIMUM_IDENTIFIER_BYTES = 1000
_REQUEST_INTERVAL_PER_BASE_SECONDS = 0.2
_MAXIMUM_PACING_WAIT_SECONDS = 2.0
_RATE_LIMIT_COOLDOWN_SECONDS = 30.0
_PRUNED_PACER_ENTRY_THRESHOLD = 256
_ITERATOR_UNAVAILABLE_ERROR_TYPE = "LIST_RECORDS_ITERATOR_NOT_AVAILABLE"
_PARTIAL_SUCCESS_REASONS = ("attachmentsFailedUploading", "attachmentUploadRateIsTooHigh")
_ERROR_TYPE = re.compile(r"[A-Z][A-Z0-9_]{1,63}")
_BASE_ID = re.compile(r"app[0-9A-Za-z]{14}")
_RECORD_ID = re.compile(r"rec[0-9A-Za-z]{14}")
_RFC3339 = re.compile(r"\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}(\.\d+)?(Z|[+-]\d{2}:\d{2})")
Output = TypeVar("Output")


class _Disposition(Enum):
    RETRY = 1
    NOT_FOUND = 2
    OFFSET_EXPIRED = 3
    REJECTED = 4
    INVALID_RESPONSE = 5
    DEFECT = 6


@dataclass(frozen=True, slots=True)
class _Outcome:
    disposition: _Disposition
    failure: Failure
    retry_after_seconds: float = 0
    receipt: Receipt = Receipt()

    def branch(self, not_found: str, offset_expired: str, invalid_response: str) -> str:
        if self.disposition is _Disposition.NOT_FOUND:
            return not_found or PROVIDER_REJECTED
        if self.disposition is _Disposition.OFFSET_EXPIRED:
            return offset_expired or PROVIDER_REJECTED
        if self.disposition is _Disposition.REJECTED:
            return PROVIDER_REJECTED
        if self.disposition is _Disposition.INVALID_RESPONSE:
            return invalid_response
        return DEFECT

    def as_query(self, empty: Output, not_found: str = "", offset_expired: str = "") -> QueryAttempt[Output]:
        if self.disposition is _Disposition.RETRY:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        branch = self.branch(not_found, offset_expired, INVALID_RESPONSE)
        return QueryAttempt.branch_result(branch, empty, self.receipt, self.failure)

    def as_mutation(self, empty: Output, not_found: str = "") -> MutationAttempt[Output]:
        if self.disposition is _Disposition.RETRY:
            return MutationAttempt.retry(self.failure, self.retry_after_seconds)
        branch = self.branch(not_found, "", INVALID_RESPONSE)
        return MutationAttempt.branch_result(branch, empty, self.receipt, self.failure)


class _BaseRequestPacer:
    """Space one process's requests to each base and hold a base back after a 429."""

    def __init__(self) -> None:
        self._next_request_at: dict[str, float] = {}

    async def wait_for_request_slot(self, base_id: str) -> float:
        """Wait for the base's next slot, or return a too-distant slot's delay for Retry."""
        now = time.monotonic()
        self._prune_idle_bases(now)
        slot = max(now, self._next_request_at.get(base_id, now))
        delay = slot - now
        if delay > _MAXIMUM_PACING_WAIT_SECONDS:
            return delay
        self._next_request_at[base_id] = slot + _REQUEST_INTERVAL_PER_BASE_SECONDS
        if delay > 0:
            await asyncio.sleep(delay)
        return 0

    def hold_after_rate_limit(self, base_id: str) -> None:
        """Delay every later request to the base until Airtable's cooldown has passed."""
        resume_at = time.monotonic() + _RATE_LIMIT_COOLDOWN_SECONDS
        if resume_at > self._next_request_at.get(base_id, 0):
            self._next_request_at[base_id] = resume_at

    def _prune_idle_bases(self, now: float) -> None:
        if len(self._next_request_at) < _PRUNED_PACER_ENTRY_THRESHOLD:
            return
        for base_id in [base_id for base_id, next_at in self._next_request_at.items() if next_at <= now]:
            del self._next_request_at[base_id]


class Client:
    """One redirect-safe HTTP client, shared per-base pacing, and a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Airtable configuration is required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 0:
            raise ValueError("Airtable maxResponseBytes must be positive")
        if credentials is None:
            raise ValueError("Airtable credential provider is required")
        self.base_url = validate_base_url(config.endpoint or _DEFAULT_ENDPOINT)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(_REQUEST_TIMEOUT_SECONDS)
        self.pacer = _BaseRequestPacer()

    def list_records(self) -> "ListRecordsOperation":
        """Bind the listRecords Query to this client."""
        return ListRecordsOperation(self)

    def get_record(self) -> "GetRecordOperation":
        """Bind the getRecord Query to this client."""
        return GetRecordOperation(self)

    def upsert_records(self) -> "UpsertRecordsOperation":
        """Bind the upsertRecords Mutation to this client."""
        return UpsertRecordsOperation(self)

    def update_records(self) -> "UpdateRecordsOperation":
        """Bind the updateRecords Mutation to this client."""
        return UpdateRecordsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        base_id: str,
        body: bytes | None,
        object_id: str,
    ) -> bytes | _Outcome:
        """Pace, authenticate, and send one request; return the 2xx body or the classified outcome."""
        pacing_delay = await self.pacer.wait_for_request_slot(base_id)
        if pacing_delay > 0:
            return _Outcome(
                _Disposition.RETRY,
                _failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    "Airtable requests to this base are paced to its rate limit",
                ),
                pacing_delay,
            )
        try:
            credentials = await self.credentials.resolve(call)
        except ReauthorizationRequiredError:
            return _Outcome(
                _Disposition.REJECTED,
                _failure(
                    operation, FailureKind.AUTHENTICATION, "the Airtable connection must be authorized again"
                ),
            )
        except (ValueError, LookupError, OSError):
            credentials = None
        token = "" if credentials is None else credentials.personal_access_token.reveal()
        if not token or not is_header_safe_credential(token):
            return _Outcome(
                _Disposition.DEFECT,
                _failure(
                    operation, FailureKind.AUTHENTICATION, "Airtable connection credentials are unavailable"
                ),
            )
        receipt = self.receipt(call, object_id)
        headers = {"Accept": "application/json", "Authorization": "Bearer " + token}
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=body, headers=headers
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                _Disposition.DEFECT,
                _failure(operation, FailureKind.LOCAL_DEFECT, "Airtable request could not be built"),
                receipt=receipt,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _transport_outcome(operation, receipt)
        try:
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Outcome(
                    _Disposition.INVALID_RESPONSE,
                    _failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Airtable response exceeds the configured size limit",
                    ),
                    receipt=receipt,
                )
            except ProviderTransportError:
                return _transport_outcome(operation, receipt)
            status = response.status_code
            if 200 <= status < 300:
                return response_body
            if status == 429:
                self.pacer.hold_after_rate_limit(base_id)
            outcome = _classify_error_response(operation, status, response.headers, response_body)
            return _Outcome(outcome.disposition, outcome.failure, outcome.retry_after_seconds, receipt)
        finally:
            await response.aclose()

    def receipt(self, call: Call, object_id: str) -> Receipt:
        """Correlate an attempt with the call, idempotency key, and Airtable object."""
        return Receipt(
            call_id=call.id,
            provider=CONNECTOR_ID,
            observed_at=datetime.now(UTC),
            idempotency_key=call.idempotency_key or IdempotencyKey(""),
            provider_object_id=object_id,
        )


class ListRecordsOperation:
    """List one bounded page of a table's records through Airtable's POST listRecords form."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Airtable client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_RECORDS_DEFINITION

    async def invoke(self, call: Call, input: ListRecordsInput) -> QueryAttempt[RecordPage]:
        """Validate and build the request body, list once, then select a branch or safe retry."""
        operation = LIST_RECORDS_OPERATION_ID
        try:
            body, page_size = _build_list_records_body(input)
        except _ValidationError as error:
            return _query_failure(operation, RecordPage(), DEFECT, FailureKind.VALIDATION, str(error))
        object_id = f"{input.base_id}/{input.table_id_or_name}"
        response_body = await self.client.exchange(
            call,
            operation,
            "POST",
            _record_path(input.base_id, input.table_id_or_name) + "/listRecords",
            input.base_id,
            _dump_json(body),
            object_id,
        )
        if isinstance(response_body, _Outcome):
            return response_body.as_query(RecordPage(), offset_expired=OFFSET_EXPIRED)
        receipt = self.client.receipt(call, object_id)
        try:
            page = _decode_record_page(response_body, page_size, body.get("filterByFormula", ""))
        except _DecodeError as error:
            return QueryAttempt.branch_result(
                INVALID_RESPONSE, RecordPage(), receipt, _failure(operation, FailureKind.PROTOCOL, str(error))
            )
        return QueryAttempt.branch_result(LISTED, page, receipt)


class GetRecordOperation:
    """Read one record by record ID; Airtable falls back to a base-wide lookup for another table's record."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Airtable client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_RECORD_DEFINITION

    async def invoke(self, call: Call, input: GetRecordInput) -> QueryAttempt[Record]:
        """Validate the IDs, read once, then select a branch or safe retry."""
        operation = GET_RECORD_OPERATION_ID
        try:
            if not isinstance(input, GetRecordInput):
                raise _ValidationError("getRecord input is invalid")
            _validate_base_and_table(input.base_id, input.table_id_or_name)
            if not _matches(_RECORD_ID, input.record_id):
                raise _ValidationError(
                    "recordId must be an Airtable record ID: rec followed by 14 letters and digits"
                )
        except _ValidationError as error:
            return _query_failure(operation, _empty_record(), DEFECT, FailureKind.VALIDATION, str(error))
        response_body = await self.client.exchange(
            call,
            operation,
            "GET",
            _record_path(input.base_id, input.table_id_or_name) + "/" + _escape(input.record_id),
            input.base_id,
            None,
            input.record_id,
        )
        if isinstance(response_body, _Outcome):
            return response_body.as_query(_empty_record(), not_found=NOT_FOUND)
        receipt = self.client.receipt(call, input.record_id)
        try:
            record = _convert_provider_record(_decode_json(response_body))
            if record.id != input.record_id:
                raise _DecodeError("Airtable returned a different record than requested")
        except _DecodeError as error:
            return QueryAttempt.branch_result(
                INVALID_RESPONSE,
                _empty_record(),
                receipt,
                _failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result(FOUND, record, receipt)


class UpsertRecordsOperation:
    """Create or update up to 10 records by merge fields, so a repeated write updates what it created."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Airtable client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and sync durability, which never overlaps two attempts."""
        return UPSERT_RECORDS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertRecordsInput) -> IdempotencyKey:
        """Derive the call's key from the call ID; Airtable accepts none, so the merge fields converge."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertRecordsInput) -> MutationAttempt[UpsertedRecords]:
        """Send one PATCH with performUpsert; an attempt that may have applied is retried safely."""
        operation = UPSERT_RECORDS_OPERATION_ID
        try:
            body = _build_upsert_records_body(input)
        except _ValidationError as error:
            return _mutation_failure(operation, UpsertedRecords(), str(error))
        object_id = f"{input.base_id}/{input.table_id_or_name}"
        response_body = await self.client.exchange(
            call,
            operation,
            "PATCH",
            _record_path(input.base_id, input.table_id_or_name),
            input.base_id,
            body,
            object_id,
        )
        if isinstance(response_body, _Outcome):
            return response_body.as_mutation(UpsertedRecords())
        receipt = self.client.receipt(call, object_id)
        try:
            upserted = _decode_upserted_records(response_body, len(input.records))
        except _DecodeError as error:
            return MutationAttempt.branch_result(
                INVALID_RESPONSE,
                UpsertedRecords(),
                receipt,
                _failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return MutationAttempt.branch_result(UPSERTED, upserted, receipt)


class UpdateRecordsOperation:
    """Set the named fields of up to 10 records by record ID; repeating absolute values is safe."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Airtable client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return UPDATE_RECORDS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateRecordsInput) -> IdempotencyKey:
        """Derive the call's key from the call ID; Airtable accepts none, so absolute values converge."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateRecordsInput) -> MutationAttempt[UpdatedRecords]:
        """Send one PATCH; an attempt that may have applied is retried safely."""
        operation = UPDATE_RECORDS_OPERATION_ID
        try:
            body = _build_update_records_body(input)
        except _ValidationError as error:
            return _mutation_failure(operation, UpdatedRecords(), str(error))
        object_id = f"{input.base_id}/{input.table_id_or_name}"
        response_body = await self.client.exchange(
            call,
            operation,
            "PATCH",
            _record_path(input.base_id, input.table_id_or_name),
            input.base_id,
            body,
            object_id,
        )
        if isinstance(response_body, _Outcome):
            return response_body.as_mutation(UpdatedRecords(), not_found=NOT_FOUND)
        receipt = self.client.receipt(call, object_id)
        try:
            updated = _decode_updated_records(
                response_body,
                [record["id"] if isinstance(record, dict) else record.id for record in input.records],
            )
        except _DecodeError as error:
            return MutationAttempt.branch_result(
                INVALID_RESPONSE,
                UpdatedRecords(),
                receipt,
                _failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return MutationAttempt.branch_result(UPDATED, updated, receipt)


class _ValidationError(ValueError):
    pass


class _DecodeError(ValueError):
    pass


def build_filter_formula(filters: tuple[FieldEqualityFilter, ...], raw_formula: str) -> str:
    """Join escaped typed equality filters and a raw formula with AND, as the Go connector does.

    Raises ValueError for an invalid filter or an oversized formula; returns "" when there is neither.
    """
    if len(filters) > MAXIMUM_FIELD_EQUALITY_FILTERS:
        raise _ValidationError(f"at most {MAXIMUM_FIELD_EQUALITY_FILTERS} field filters are allowed")
    if not isinstance(raw_formula, str) or len(raw_formula.encode()) > MAXIMUM_FORMULA_BYTES:
        raise _ValidationError(f"formula is limited to {MAXIMUM_FORMULA_BYTES} bytes")
    conditions = []
    for index, equality_filter in enumerate(filters):
        try:
            conditions.append(_formula_condition(equality_filter))
        except _ValidationError as error:
            raise _ValidationError(f"filters[{index}]: {error}") from None
    if raw_formula.strip():
        conditions.append("(" + raw_formula + ")")
    if not conditions:
        formula = ""
    elif len(conditions) == 1:
        formula = conditions[0]
    else:
        formula = "AND(" + ", ".join(conditions) + ")"
    if len(formula.encode()) > MAXIMUM_FORMULA_BYTES:
        raise _ValidationError(f"the combined filter formula is limited to {MAXIMUM_FORMULA_BYTES} bytes")
    return formula


def _formula_condition(equality_filter: FieldEqualityFilter) -> str:
    if not isinstance(equality_filter, FieldEqualityFilter):
        raise _ValidationError("filter is invalid")
    _validate_identifier("field", equality_filter.field)
    if any(character in equality_filter.field for character in "{}\\"):
        raise _ValidationError("field name cannot contain {, }, or a backslash; use the field ID")
    reference = "{" + equality_filter.field + "}"
    values = (equality_filter.text, equality_filter.number, equality_filter.checkbox)
    if sum(value is not None for value in values) != 1:
        raise _ValidationError("exactly one of text, number, and checkbox must be set")
    if equality_filter.text is not None:
        text = equality_filter.text
        if "\\" in text:
            raise _ValidationError("text cannot contain a backslash; use formula")
        if any(unicodedata.category(character) == "Cc" for character in text):
            raise _ValidationError("text cannot contain control characters such as line breaks; use formula")
        return reference + ' = "' + text.replace('"', '\\"') + '"'
    if equality_filter.number is not None:
        number = equality_filter.number
        if isinstance(number, bool) or not isinstance(number, int | float) or not math.isfinite(number):
            raise _ValidationError("number must be finite")
        return reference + " = " + _format_number(number)
    if equality_filter.checkbox:
        return reference + " = TRUE()"
    return "NOT(" + reference + ")"


def _format_number(number: float) -> str:
    """Match Go strconv.FormatFloat(number, 'f', -1, 64): shortest digits, no exponent."""
    text = format(Decimal(repr(float(number))), "f")
    return text.rstrip("0").rstrip(".") if "." in text else text


def _build_list_records_body(input: ListRecordsInput) -> tuple[dict[str, Any], int]:
    if not isinstance(input, ListRecordsInput):
        raise _ValidationError("listRecords input is invalid")
    _validate_base_and_table(input.base_id, input.table_id_or_name)
    page_size = input.page_size or MAXIMUM_PAGE_SIZE
    if type(page_size) is not int or not 1 <= page_size <= MAXIMUM_PAGE_SIZE:
        raise _ValidationError(f"pageSize must be between 1 and {MAXIMUM_PAGE_SIZE}")
    if not isinstance(input.offset, str) or len(input.offset.encode()) > _MAXIMUM_OFFSET_BYTES:
        raise _ValidationError(f"offset is limited to {_MAXIMUM_OFFSET_BYTES} bytes")
    filters = tuple(
        FieldEqualityFilter(**filter) if isinstance(filter, dict) else filter for filter in input.filters
    )
    formula = build_filter_formula(filters, input.formula)
    if len(input.fields) > MAXIMUM_LISTED_FIELDS:
        raise _ValidationError(f"at most {MAXIMUM_LISTED_FIELDS} fields can be listed")
    fields = _unique_identifiers("fields", input.fields)
    if len(input.sort) > MAXIMUM_RECORD_SORTS:
        raise _ValidationError(f"at most {MAXIMUM_RECORD_SORTS} sort entries are allowed")
    sorts = tuple(RecordSort(**sort) if isinstance(sort, dict) else sort for sort in input.sort)
    for index, sort in enumerate(sorts):
        _validate_identifier(f"sort[{index}].field", sort.field)
        if sort.direction not in (None, "", "asc", "desc"):
            raise _ValidationError(f"sort[{index}].direction must be asc or desc")
    if input.view_id_or_name:
        _validate_identifier("viewIdOrName", input.view_id_or_name)
    body: dict[str, Any] = {"pageSize": page_size}
    if input.offset:
        body["offset"] = input.offset
    if formula:
        body["filterByFormula"] = formula
    if fields:
        body["fields"] = fields
    if sorts:
        body["sort"] = [
            {"field": sort.field, **({"direction": sort.direction} if sort.direction else {})}
            for sort in sorts
        ]
    if input.view_id_or_name:
        body["view"] = input.view_id_or_name
    return body, page_size


def _build_upsert_records_body(input: UpsertRecordsInput) -> bytes:
    if not isinstance(input, UpsertRecordsInput):
        raise _ValidationError("upsertRecords input is invalid")
    _validate_base_and_table(input.base_id, input.table_id_or_name)
    if not 1 <= len(input.fields_to_merge_on) <= MAXIMUM_MERGE_FIELDS:
        raise _ValidationError(f"fieldsToMergeOn must name 1 to {MAXIMUM_MERGE_FIELDS} fields")
    merge_fields = _unique_identifiers("fieldsToMergeOn", input.fields_to_merge_on)
    if len(merge_fields) != len(input.fields_to_merge_on):
        raise _ValidationError("fieldsToMergeOn cannot repeat a field")
    if not 1 <= len(input.records) <= MAXIMUM_RECORDS_PER_WRITE:
        raise _ValidationError(f"records must hold 1 to {MAXIMUM_RECORDS_PER_WRITE} records")
    seen_merge_keys: dict[str, int] = {}
    records = []
    for index, record in enumerate(input.records):
        if isinstance(record, dict):
            record = RecordUpsert(**record)
        label = f"records[{index}]"
        fields = _validate_written_fields(label, record.fields)
        parts = []
        for merge_field in merge_fields:
            if fields.get(merge_field) is None:
                raise _ValidationError(f'{label} needs a non-null value for merge field "{merge_field}"')
            parts.append(_compact_json(fields[merge_field]))
        merge_key = "\x00".join(parts)
        if merge_key in seen_merge_keys:
            raise _ValidationError(
                f"{label} repeats the merge field values of records[{seen_merge_keys[merge_key]}]"
            )
        seen_merge_keys[merge_key] = index
        records.append({"fields": dict(fields)})
    body: dict[str, Any] = {"performUpsert": {"fieldsToMergeOn": merge_fields}, "records": records}
    if input.typecast:
        body["typecast"] = True
    return _dump_json(body)


def _build_update_records_body(input: UpdateRecordsInput) -> bytes:
    if not isinstance(input, UpdateRecordsInput):
        raise _ValidationError("updateRecords input is invalid")
    _validate_base_and_table(input.base_id, input.table_id_or_name)
    if not 1 <= len(input.records) <= MAXIMUM_RECORDS_PER_WRITE:
        raise _ValidationError(f"records must hold 1 to {MAXIMUM_RECORDS_PER_WRITE} records")
    seen_record_ids: set[str] = set()
    records = []
    for index, record in enumerate(input.records):
        if isinstance(record, dict):
            record = RecordUpdate(**record)
        label = f"records[{index}]"
        if not _matches(_RECORD_ID, record.id):
            raise _ValidationError(
                f"{label}.id must be an Airtable record ID: rec followed by 14 letters and digits"
            )
        if record.id in seen_record_ids:
            raise _ValidationError(f"{label} repeats record {record.id}")
        seen_record_ids.add(record.id)
        fields = _validate_written_fields(label, record.fields)
        records.append({"id": record.id, "fields": dict(fields)})
    body: dict[str, Any] = {"records": records}
    if input.typecast:
        body["typecast"] = True
    return _dump_json(body)


def _validate_written_fields(label: str, fields: dict[str, Any] | None) -> dict[str, Any]:
    if not isinstance(fields, dict) or not fields:
        raise _ValidationError(f"{label} needs at least one field value")
    if len(fields) > MAXIMUM_FIELDS_PER_RECORD:
        raise _ValidationError(f"{label} can set at most {MAXIMUM_FIELDS_PER_RECORD} fields")
    for name in sorted(fields):
        try:
            _validate_identifier("field name", name)
        except _ValidationError as error:
            raise _ValidationError(f"{label}: {error}") from None
        try:
            _compact_json(fields[name])
        except (TypeError, ValueError):
            raise _ValidationError(f'{label}: field "{name}" holds no valid JSON cell value') from None
    return fields


def _validate_base_and_table(base_id: object, table_id_or_name: object) -> None:
    if not _matches(_BASE_ID, base_id):
        raise _ValidationError("baseId must be an Airtable base ID: app followed by 14 letters and digits")
    _validate_identifier("tableIdOrName", table_id_or_name)


def _validate_identifier(label: str, value: object) -> None:
    """Check a table, view, or field name or ID that Airtable matches literally."""
    if not isinstance(value, str) or not value:
        raise _ValidationError(f"{label} is required")
    try:
        encoded = value.encode()
    except UnicodeEncodeError:
        encoded = b""
    if not encoded or len(encoded) > _MAXIMUM_IDENTIFIER_BYTES:
        raise _ValidationError(f"{label} must be valid UTF-8 of at most {_MAXIMUM_IDENTIFIER_BYTES} bytes")
    if any(unicodedata.category(character) == "Cc" for character in value):
        raise _ValidationError(f"{label} cannot contain control characters")


def _unique_identifiers(label: str, values: tuple[str, ...]) -> list[str]:
    unique: list[str] = []
    for index, value in enumerate(values):
        _validate_identifier(f"{label}[{index}]", value)
        if value not in unique:
            unique.append(value)
    return unique


def _classify_error_response(operation: str, status: int, headers: httpx.Headers, body: bytes) -> _Outcome:
    error_type = _read_error_type(body)
    detail = f", {error_type}" if error_type else ""
    retry_after = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
    if status == 429:
        return _Outcome(
            _Disposition.RETRY,
            _failure(operation, FailureKind.RATE_LIMIT, "Airtable rate limited requests to this base"),
            max(retry_after, _RATE_LIMIT_COOLDOWN_SECONDS),
        )
    if status == 408 or (status >= 500 and status != 501):
        return _Outcome(
            _Disposition.RETRY,
            _failure(
                operation,
                FailureKind.AVAILABILITY,
                f"Airtable is temporarily unavailable (HTTP {status}{detail})",
            ),
            retry_after,
        )
    if status == 422 and error_type == _ITERATOR_UNAVAILABLE_ERROR_TYPE:
        return _terminal(
            _Disposition.OFFSET_EXPIRED,
            operation,
            FailureKind.NOT_FOUND,
            f"Airtable no longer recognizes the list offset ({_ITERATOR_UNAVAILABLE_ERROR_TYPE})",
        )
    if status == 401:
        return _terminal(
            _Disposition.REJECTED,
            operation,
            FailureKind.AUTHENTICATION,
            f"Airtable rejected the personal access token (HTTP 401{detail})",
        )
    if status == 403:
        return _terminal(
            _Disposition.REJECTED,
            operation,
            FailureKind.AUTHORIZATION,
            "Airtable denied access: check the token's scopes and bases, or the base, table, or field name "
            f"(HTTP 403{detail})",
        )
    if status == 404:
        return _terminal(
            _Disposition.NOT_FOUND,
            operation,
            FailureKind.NOT_FOUND,
            f"Airtable found no such record or route (HTTP 404{detail})",
        )
    if status in (400, 413, 422):
        return _terminal(
            _Disposition.REJECTED,
            operation,
            FailureKind.VALIDATION,
            f"Airtable rejected the request as invalid (HTTP {status}{detail})",
        )
    if 300 <= status < 400:
        return _terminal(
            _Disposition.REJECTED,
            operation,
            FailureKind.PROTOCOL,
            f"Airtable redirected the request (HTTP {status})",
        )
    return _terminal(
        _Disposition.REJECTED,
        operation,
        FailureKind.PROVIDER_REJECTION,
        f"Airtable rejected the request (HTTP {status}{detail})",
    )


def _read_error_type(body: bytes) -> str:
    """Return Airtable's uppercase error type from /error/type or /error, never its message."""
    if len(body) > MAX_ERROR_BODY_BYTES:
        return ""
    for token in read_error_tokens(body, ("/error/type", "/error")):
        if _ERROR_TYPE.fullmatch(token):
            return token
    return ""


def _decode_record_page(body: bytes, page_size: int, filter_formula: str) -> RecordPage:
    document = _decode_json(body)
    if not isinstance(document, dict):
        raise _DecodeError("Airtable returned a malformed response")
    records = document.get("records")
    offset = _typed(document.get("offset"), str, "")
    if records is None:
        raise _DecodeError("Airtable returned a list response without records")
    if not isinstance(records, list):
        raise _DecodeError("Airtable returned a malformed response")
    if len(records) > page_size:
        raise _DecodeError("Airtable returned more records than the requested page size")
    if len(offset.encode()) > _MAXIMUM_OFFSET_BYTES:
        raise _DecodeError("Airtable returned an oversized offset")
    return RecordPage(tuple(_convert_provider_record(record) for record in records), offset, filter_formula)


def _decode_upserted_records(body: bytes, requested_records: int) -> UpsertedRecords:
    document = _decode_json(body)
    if not isinstance(document, dict):
        raise _DecodeError("Airtable returned a malformed response")
    records, created, updated = (
        document.get(name) for name in ("records", "createdRecords", "updatedRecords")
    )
    if records is None or created is None or updated is None:
        raise _DecodeError(
            "Airtable returned an upsert response without records, createdRecords, and updatedRecords"
        )
    if not isinstance(records, list):
        raise _DecodeError("Airtable returned a malformed response")
    created_ids, updated_ids = _string_list(created), _string_list(updated)
    if len(records) != requested_records:
        raise _DecodeError("Airtable returned a different number of records than the upsert wrote")
    converted = tuple(_convert_provider_record(record) for record in records)
    written_ids = {record.id for record in converted}
    if any(record_id not in written_ids for record_id in created_ids + updated_ids):
        raise _DecodeError("Airtable listed a created or updated record that the upsert did not return")
    if any(record_id in updated_ids for record_id in created_ids):
        raise _DecodeError("Airtable listed one record as both created and updated")
    return UpsertedRecords(
        converted,
        tuple(created_ids),
        tuple(updated_ids),
        _known_partial_success_reasons(document.get("details")),
    )


def _decode_updated_records(body: bytes, requested_record_ids: list[str]) -> UpdatedRecords:
    document = _decode_json(body)
    if not isinstance(document, dict):
        raise _DecodeError("Airtable returned a malformed response")
    records = document.get("records")
    if records is None:
        raise _DecodeError("Airtable returned an update response without records")
    if not isinstance(records, list):
        raise _DecodeError("Airtable returned a malformed response")
    converted = tuple(_convert_provider_record(record) for record in records)
    if len(converted) != len(requested_record_ids):
        raise _DecodeError("Airtable returned a different number of records than the update wrote")
    if any(record.id not in requested_record_ids for record in converted):
        raise _DecodeError("Airtable returned a record the update did not name")
    return UpdatedRecords(converted, _known_partial_success_reasons(document.get("details")))


def _empty_record() -> Record:
    return Record(fields={})


def _convert_provider_record(document: Any) -> Record:
    """Validate one provider record and convert it, exactly as Go's convertProviderRecord."""
    if document is None:
        document = {}
    if not isinstance(document, dict):
        raise _DecodeError("Airtable returned a malformed response")
    record_id = _typed(document.get("id"), str, "")
    created_time = _typed(document.get("createdTime"), str, "")
    fields = document.get("fields")
    if fields is not None and not isinstance(fields, dict):
        raise _DecodeError("Airtable returned a malformed response")
    if not _RECORD_ID.fullmatch(record_id):
        raise _DecodeError("Airtable returned a record without a valid record ID")
    parsed = _parse_rfc3339(created_time)
    if parsed is None:
        raise _DecodeError("Airtable returned a record without a valid createdTime")
    return Record(record_id, parsed, dict(fields or {}))


def _known_partial_success_reasons(details: Any) -> tuple[str, ...]:
    """Keep the documented reasons of a partial success and drop any other text."""
    if details is None:
        return ()
    if not isinstance(details, dict):
        raise _DecodeError("Airtable returned a malformed response")
    message = _typed(details.get("message"), str, "")
    reasons = details.get("reasons")
    reasons = _string_list(reasons) if reasons is not None else []
    if message != "partialSuccess":
        return ()
    known: list[str] = []
    for reason in reasons:
        if reason in _PARTIAL_SUCCESS_REASONS and reason not in known:
            known.append(reason)
    return tuple(known)


def _string_list(value: Any) -> list[str]:
    if value is None:
        return []
    if not isinstance(value, list) or not all(isinstance(item, str) for item in value):
        raise _DecodeError("Airtable returned a malformed response")
    return list(value)


def _parse_rfc3339(value: str) -> datetime | None:
    if _RFC3339.fullmatch(value) is None:
        return None
    try:
        return datetime.fromisoformat(value).astimezone(UTC)
    except ValueError:
        return None


def _record_path(base_id: str, table_id_or_name: str) -> str:
    return "/v0/" + _escape(base_id) + "/" + _escape(table_id_or_name)


def _escape(segment: str) -> str:
    """Escape one path segment like Go's url.PathEscape."""
    return quote(segment, safe="$&+:=@")


def _matches(pattern: re.Pattern[str], value: object) -> bool:
    return isinstance(value, str) and pattern.fullmatch(value) is not None


def _compact_json(value: object) -> str:
    return json.dumps(value, separators=(",", ":"), allow_nan=False, ensure_ascii=False)


def _dump_json(value: object) -> bytes:
    return _compact_json(value).encode()


def _decode_json(body: bytes) -> Any:
    try:
        return json.loads(body)
    except (ValueError, UnicodeDecodeError) as error:
        raise _DecodeError("Airtable returned a malformed response") from error


def _typed(value: Any, expected: type, default: Any) -> Any:
    if value is None:
        return default
    if type(value) is not expected:
        raise _DecodeError("Airtable returned a malformed response")
    return value


def _transport_outcome(operation: str, receipt: Receipt) -> _Outcome:
    return _Outcome(
        _Disposition.RETRY,
        _failure(operation, FailureKind.TRANSPORT, "Airtable did not return a complete response"),
        receipt=receipt,
    )


def _terminal(disposition: _Disposition, operation: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(disposition, _failure(operation, kind, message))


def _query_failure(
    operation: str, value: Output, branch: str, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(branch, value, Receipt(), _failure(operation, kind, message))


def _mutation_failure(operation: str, value: Output, message: str) -> MutationAttempt[Output]:
    return MutationAttempt.branch_result(
        DEFECT, value, Receipt(), _failure(operation, FailureKind.VALIDATION, message)
    )


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)
