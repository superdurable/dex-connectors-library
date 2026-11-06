# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Salesforce operation validation, provider decoding, and Dex outcomes."""

from __future__ import annotations

import json
import re
from datetime import UTC, datetime
from decimal import Decimal
from pathlib import PurePosixPath
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

from .client import Client, Outcome, failure, go_query, is_api_name, is_record_id
from .definitions import (
    GET_RECORD_DEFINITION,
    QUERY_RECORDS_DEFINITION,
    UPDATE_RECORD_DEFINITION,
    UPSERT_RECORD_BY_EXTERNAL_ID_DEFINITION,
)
from .models import (
    GetRecordInput,
    QueryRecordsInput,
    QueryRecordsOutput,
    Record,
    SOQLValue,
    UpdateRecordInput,
    UpdateRecordOutput,
    UpsertRecordByExternalIDInput,
    UpsertRecordByExternalIDOutput,
)

_NUMBER = re.compile(r"-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?\Z")
_NAME = re.compile(r"[A-Za-z_][A-Za-z0-9_]{0,63}\Z")
_IDENTIFIER = re.compile(r"[A-Za-z][A-Za-z0-9_]*(?:\.[A-Za-z][A-Za-z0-9_]*){0,4}\Z")
_LOCATOR = re.compile(r"[A-Za-z0-9]{15,18}-[0-9]{1,10}\Z")
_RFC3339 = re.compile(r"(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2}):(\d{2})(?:\.(\d+))?(Z|[+-]\d{2}:\d{2})\Z")
T = TypeVar("T")


def _safe_json_number(value: Decimal, field_name: str) -> float:
    try:
        number = float(value)
    except (OverflowError, ValueError) as error:
        raise ValueError(f"field {field_name} has an unrepresentable JSON number") from error
    if not number.is_integer() and Decimal(str(number)) != value or not (-1e308 < number < 1e308):
        raise ValueError(f"field {field_name} has an unrepresentable JSON number")
    return number


def _convert_json_value(value: Any, field_name: str) -> Any:
    if isinstance(value, Decimal):
        return _safe_json_number(value, field_name)
    if isinstance(value, dict):
        return {key: _convert_json_value(item, key) for key, item in value.items()}
    if isinstance(value, list):
        return [_convert_json_value(item, field_name) for item in value]
    return value


def _json_document(body: bytes) -> Any:
    return json.loads(
        body,
        parse_float=Decimal,
        parse_constant=lambda _: (_ for _ in ()).throw(ValueError("non-finite JSON number")),
    )


def _decode_record(value: Any) -> Record:
    if not isinstance(value, dict):
        raise ValueError("record is not a JSON object")
    attributes = value.get("attributes")
    if not isinstance(attributes, dict) or not isinstance(attributes.get("type"), str):
        raise ValueError("record attributes lack a valid sObject type")
    record_type = attributes["type"]
    if not is_api_name(record_type) and record_type != "AggregateResult":
        raise ValueError("record attributes lack a valid sObject type")
    fields = {key: _convert_json_value(item, key) for key, item in value.items() if key != "attributes"}
    record_id = fields.get("Id", "")
    if record_id is None:
        record_id = ""
    if record_id and (not isinstance(record_id, str) or not is_record_id(record_id)):
        raise ValueError("record Id is invalid")
    return Record(record_type, record_id, fields)


def _encode_fields(fields: dict[str, Any] | None, *excluded: str) -> bytes:
    values = fields or {}
    if len(values) > 500:
        raise ValueError("fields must hold at most 500 field values")
    seen: set[str] = set()
    for name in values:
        if not is_api_name(name):
            raise ValueError("fields keys must be field API names such as LastName")
        lower_name = name.lower()
        if lower_name in ("id", "attributes") or lower_name in seen:
            raise ValueError("fields must not set Id or attributes or repeat a field API name")
        if any(lower_name == item.lower() for item in excluded):
            raise ValueError(f"fields must not set {excluded[0]}, which the request path already names")
        seen.add(lower_name)
        try:
            json.dumps(values[name], allow_nan=False)
        except (TypeError, ValueError, OverflowError) as error:
            raise ValueError(f"field {name} must hold one JSON value") from error
    body = json.dumps(
        values, allow_nan=False, sort_keys=True, separators=(",", ":"), ensure_ascii=False
    ).encode()
    if len(body) > 1 << 20:
        raise ValueError("fields exceed the 1 MiB request limit")
    return body


def _quote_soql(value: str, *, like: bool = False, prefix: str = "", suffix: str = "") -> str:
    output = ["'", prefix]
    for character in value:
        escaped = {
            "\\": "\\\\",
            "'": "\\'",
            '"': '\\"',
            "\n": "\\n",
            "\r": "\\r",
            "\t": "\\t",
            "\b": "\\b",
            "\f": "\\f",
        }.get(character)
        if escaped is not None:
            output.append(escaped)
        elif character in "%_" and like:
            output.append("\\" + character)
        elif ord(character) < 32 or ord(character) == 127:
            raise ValueError("text contains a control character SOQL cannot escape")
        else:
            output.append(character)
    return "".join(output) + suffix + "'"


def _render_binding(value: SOQLValue | dict[str, Any]) -> str:
    if isinstance(value, dict):
        value = SOQLValue(**value)
    if value.kind == "string":
        return _quote_soql(value.text)
    if value.kind in ("likeContains", "likeStartsWith"):
        return _quote_soql(
            value.text, like=True, prefix="%" if value.kind == "likeContains" else "", suffix="%"
        )
    if value.kind == "stringList":
        if not 1 <= len(value.texts) <= 1000:
            raise ValueError("stringList must hold from 1 to 1000 values")
        return "(" + ", ".join(_quote_soql(item) for item in value.texts) + ")"
    if value.kind == "number":
        if len(value.text) > 40 or not _NUMBER.fullmatch(value.text):
            raise ValueError("number must be a plain decimal such as -12 or 1250.50")
        return value.text
    if value.kind == "boolean":
        return "TRUE" if value.boolean else "FALSE"
    if value.kind == "null":
        return "NULL"
    if value.kind == "identifier":
        if len(value.text.encode()) > 255 or not _IDENTIFIER.fullmatch(value.text):
            raise ValueError("identifier must be an API name or a dotted relationship path")
        return value.text
    if value.kind == "date":
        if not re.fullmatch(r"\d{4}-\d{2}-\d{2}", value.text):
            raise ValueError("date must be YYYY-MM-DD")
        try:
            datetime.strptime(value.text, "%Y-%m-%d")
        except ValueError as error:
            raise ValueError("date must be YYYY-MM-DD") from error
        return value.text
    if value.kind == "dateTime":
        match = _RFC3339.fullmatch(value.text)
        if not match or int(match[4]) > 23 or int(match[5]) > 59 or int(match[6]) > 59:
            raise ValueError("dateTime must be an RFC 3339 timestamp")
        try:
            parsed = datetime.fromisoformat(value.text)
        except ValueError as error:
            raise ValueError("dateTime must be an RFC 3339 timestamp") from error
        return parsed.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
    raise ValueError("kind is not a supported SOQL value kind")


def render_soql(statement: str, bindings: dict[str, SOQLValue]) -> str:
    """Substitute only named placeholders outside SOQL string literals."""
    if not statement.strip():
        raise ValueError("soql is required")
    if len(bindings) > 100:
        raise ValueError("soql accepts at most 100 bindings")
    if any(not _NAME.fullmatch(name) for name in bindings):
        raise ValueError("binding names must match the supported pattern")
    rendered: list[str] = []
    used: set[str] = set()
    in_literal = False
    index = 0
    while index < len(statement):
        character = statement[index]
        if in_literal and character == "\\" and index + 1 < len(statement):
            rendered.extend((character, statement[index + 1]))
            index += 2
            continue
        if character == "'":
            in_literal = not in_literal
        if (
            not in_literal
            and character == ":"
            and index + 1 < len(statement)
            and re.match(r"[A-Za-z_]", statement[index + 1])
        ):
            end = index + 1
            while end < len(statement) and re.match(r"[A-Za-z0-9_]", statement[end]):
                end += 1
            name = statement[index + 1 : end]
            if name not in bindings:
                raise ValueError(f"placeholder :{name} has no binding")
            try:
                rendered.append(_render_binding(bindings[name]))
            except ValueError as error:
                raise ValueError(f"binding {name}: {error}") from error
            used.add(name)
            index = end
            continue
        rendered.append(character)
        index += 1
    if in_literal:
        raise ValueError("soql has an unterminated string literal")
    unused = sorted(set(bindings) - used)
    if unused:
        raise ValueError("bindings " + ", ".join(unused) + " have no placeholder")
    return "".join(rendered)


def _query_outcome(
    client: Client, call: Call, operation: str, outcome: Outcome, empty: T, routes: dict[str, str]
) -> QueryAttempt[T]:
    branch = routes.get(outcome.route, outcome.route)
    if branch == "retry":
        return QueryAttempt.retry(outcome.failure, outcome.retry_after)
    return QueryAttempt.branch_result(
        branch, empty, client.receipt(call, api_usage=outcome.api_usage), outcome.failure
    )


def _mutation_outcome(
    client: Client, call: Call, outcome: Outcome, empty: T, routes: dict[str, str]
) -> MutationAttempt[T]:
    branch = routes.get(outcome.route, outcome.route)
    if branch == "retry":
        return MutationAttempt.retry(outcome.failure, outcome.retry_after)
    return MutationAttempt.branch_result(
        branch, empty, client.receipt(call, api_usage=outcome.api_usage), outcome.failure
    )


class QueryRecordsOperation:
    """Query one bounded SOQL page or continue a validated cursor."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest queryRecords definition."""
        return QUERY_RECORDS_DEFINITION

    async def invoke(self, call: Call, input: QueryRecordsInput) -> QueryAttempt[QueryRecordsOutput]:
        """Send a validated query and classify its page."""
        empty = QueryRecordsOutput()
        try:
            batch_size = input.batch_size or self.client.query_batch_size
            if type(batch_size) is not int or not 200 <= batch_size <= 2000:
                raise ValueError("batchSize must be zero or from 200 to 2000")
            if input.next_records_cursor:
                if input.soql.strip() or input.bindings:
                    raise ValueError("set soql or nextRecordsCursor, not both")
                prefix = self.client.data_path("query") + "/"
                cursor = input.next_records_cursor
                if not cursor.startswith(prefix) or not _LOCATOR.fullmatch(cursor[len(prefix) :]):
                    raise ValueError(
                        "nextRecordsCursor must be a cursor returned by queryRecords for this apiVersion"
                    )
                path, query = self.client.data_path("query", cursor[len(prefix) :]), None
            else:
                statement = render_soql(input.soql, input.bindings)
                query = {"q": statement}
                if len(go_query(query).encode()) > 15000:
                    raise ValueError("soql is too long for one Salesforce REST request")
                path = self.client.data_path("query")
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect", empty, Receipt(), failure("queryRecords", FailureKind.VALIDATION, str(error))
            )
        credentials, outcome = await self.client.resolve_credentials(call, "queryRecords")
        if outcome:
            return _query_outcome(self.client, call, "queryRecords", outcome, empty, {})
        assert credentials is not None
        response, outcome = await self.client.send(
            call, credentials, "queryRecords", "GET", path, query=query, query_batch_size=batch_size
        )
        if outcome:
            return _query_outcome(
                self.client,
                call,
                "queryRecords",
                outcome,
                empty,
                {"notFound": "providerRejected", "requestRejected": "queryRejected"},
            )
        assert response is not None
        receipt = self.client.receipt(call, api_usage=response.api_usage)
        try:
            document = _json_document(response.body)
            if response.status != 200 or not isinstance(document, dict):
                raise ValueError("query status or shape")
            total = document.get("totalSize")
            done = document.get("done")
            records = document.get("records")
            if records is None:
                records = []
            if type(total) is not int or total < 0 or type(done) is not bool or not isinstance(records, list):
                raise ValueError("query fields")
            next_cursor = ""
            if not done:
                next_cursor = document.get("nextRecordsUrl", "")
                prefix = self.client.data_path("query") + "/"
                if (
                    not isinstance(next_cursor, str)
                    or not next_cursor.startswith(prefix)
                    or not _LOCATOR.fullmatch(next_cursor[len(prefix) :])
                ):
                    raise ValueError("next cursor")
            output = QueryRecordsOutput(
                tuple(_decode_record(value) for value in records), total, done, next_cursor
            )
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("queryRecords", FailureKind.PROTOCOL, "Salesforce returned an invalid query page"),
            )
        if output.total_size == 0 and not output.records and output.is_done:
            return QueryAttempt.branch_result(
                "notFound",
                output,
                receipt,
                failure("queryRecords", FailureKind.NOT_FOUND, "no record matched the query"),
            )
        return QueryAttempt.branch_result("found", output, receipt)


class GetRecordOperation:
    """Read a Salesforce record by object type, ID, and selected fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest getRecord definition."""
        return GET_RECORD_DEFINITION

    async def invoke(self, call: Call, input: GetRecordInput) -> QueryAttempt[Record]:
        """Validate and decode one Salesforce sObject row."""
        empty = Record(fields={})
        if (
            not is_api_name(input.s_object_type)
            or not is_record_id(input.record_id)
            or not 1 <= len(input.fields) <= 200
            or any(not is_api_name(field) for field in input.fields)
            or len({field.lower() for field in input.fields}) != len(input.fields)
        ):
            return QueryAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("getRecord", FailureKind.VALIDATION, "record selection is invalid"),
            )
        credentials, outcome = await self.client.resolve_credentials(call, "getRecord")
        if outcome:
            return _query_outcome(self.client, call, "getRecord", outcome, empty, {})
        assert credentials is not None
        response, outcome = await self.client.send(
            call,
            credentials,
            "getRecord",
            "GET",
            self.client.data_path("sobjects", input.s_object_type, input.record_id),
            query={"fields": ",".join(input.fields)},
        )
        if outcome:
            return _query_outcome(
                self.client, call, "getRecord", outcome, empty, {"requestRejected": "providerRejected"}
            )
        assert response is not None
        receipt = self.client.receipt(call, input.record_id, response.api_usage)
        try:
            record = _decode_record(_json_document(response.body))
            if (
                response.status != 200
                or record.type.lower() != input.s_object_type.lower()
                or record.id
                and record.id[:15] != input.record_id[:15]
            ):
                raise ValueError("record identity")
        except (ValueError, TypeError, UnicodeDecodeError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                receipt,
                failure("getRecord", FailureKind.PROTOCOL, "Salesforce returned an invalid record"),
            )
        if not record.id:
            record = Record(record.type, input.record_id, record.fields)
        return QueryAttempt.branch_result(
            "found", record, self.client.receipt(call, record.id, response.api_usage)
        )


class UpsertRecordByExternalIDOperation:
    """Create or update a record by its unique external ID value."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest upsert definition."""
        return UPSERT_RECORD_BY_EXTERNAL_ID_DEFINITION

    def idempotency_key(self, call_id: CallID, _: UpsertRecordByExternalIDInput) -> IdempotencyKey:
        """Use the stable call ID; Salesforce deduplicates by external ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(
        self, call: Call, input: UpsertRecordByExternalIDInput
    ) -> MutationAttempt[UpsertRecordByExternalIDOutput]:
        """Send a repeatable PATCH and classify its result."""
        empty = UpsertRecordByExternalIDOutput()
        value = input.external_id_value
        if (
            not is_api_name(input.s_object_type)
            or not is_api_name(input.external_id_field)
            or input.external_id_field.lower() == "id"
            or not value
            or value.strip() != value
            or len(value) > 255
            or value in (".", "..")
            or any(ord(character) < 32 or ord(character) == 127 for character in value)
        ):
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure(
                    "upsertRecordByExternalId", FailureKind.VALIDATION, "external ID selection is invalid"
                ),
            )
        try:
            body = _encode_fields(input.fields, input.external_id_field)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("upsertRecordByExternalId", FailureKind.VALIDATION, str(error)),
            )
        credentials, outcome = await self.client.resolve_credentials(call, "upsertRecordByExternalId")
        if outcome:
            return _mutation_outcome(self.client, call, outcome, empty, {})
        assert credentials is not None
        response, outcome = await self.client.send(
            call,
            credentials,
            "upsertRecordByExternalId",
            "PATCH",
            self.client.data_path("sobjects", input.s_object_type, input.external_id_field, value),
            body=body,
            duplicate_retry=True,
            oversized_route="retry",
        )
        if outcome:
            return _mutation_outcome(
                self.client,
                call,
                outcome,
                UpsertRecordByExternalIDOutput(provider_errors=outcome.provider_errors),
                {"notFound": "providerRejected", "requestRejected": "recordRejected"},
            )
        assert response is not None
        receipt = self.client.receipt(call, api_usage=response.api_usage)
        if response.status == 300:
            ids: list[str] = []
            try:
                paths = _json_document(response.body)
                if isinstance(paths, list):
                    ids = [
                        PurePosixPath(item).name
                        for item in paths
                        if isinstance(item, str) and is_record_id(PurePosixPath(item).name)
                    ]
            except (ValueError, UnicodeDecodeError):
                pass
            return MutationAttempt.branch_result(
                "multipleMatches",
                UpsertRecordByExternalIDOutput(matching_record_ids=tuple(ids)),
                receipt,
                failure(
                    "upsertRecordByExternalId",
                    FailureKind.CONFLICT,
                    "more than one record carries the external ID value",
                ),
            )
        try:
            document = _json_document(response.body)
            if (
                not isinstance(document, dict)
                or document.get("success") is not True
                or type(document.get("created")) is not bool
                or not isinstance(document.get("id"), str)
                or not is_record_id(document["id"])
            ):
                raise ValueError("upsert result")
            result = UpsertRecordByExternalIDOutput(document["id"], document["created"])
        except (ValueError, UnicodeDecodeError):
            return MutationAttempt.retry(
                failure(
                    "upsertRecordByExternalId",
                    FailureKind.PROTOCOL,
                    "Salesforce returned an unreadable upsert result",
                )
            )
        return MutationAttempt.branch_result(
            "upserted", result, self.client.receipt(call, result.id, response.api_usage)
        )


class UpdateRecordOperation:
    """Replace selected field values by Salesforce record ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest update definition."""
        return UPDATE_RECORD_DEFINITION

    def idempotency_key(self, call_id: CallID, _: UpdateRecordInput) -> IdempotencyKey:
        """Use the stable call ID for a convergent absolute field update."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateRecordInput) -> MutationAttempt[UpdateRecordOutput]:
        """Send a repeatable PATCH and classify its result."""
        empty = UpdateRecordOutput()
        if not is_api_name(input.s_object_type) or not is_record_id(input.record_id) or not input.fields:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                Receipt(),
                failure("updateRecord", FailureKind.VALIDATION, "record update selection is invalid"),
            )
        try:
            body = _encode_fields(input.fields)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect", empty, Receipt(), failure("updateRecord", FailureKind.VALIDATION, str(error))
            )
        credentials, outcome = await self.client.resolve_credentials(call, "updateRecord")
        if outcome:
            return _mutation_outcome(self.client, call, outcome, empty, {})
        assert credentials is not None
        response, outcome = await self.client.send(
            call,
            credentials,
            "updateRecord",
            "PATCH",
            self.client.data_path("sobjects", input.s_object_type, input.record_id),
            body=body,
            oversized_route="retry",
        )
        if outcome:
            return _mutation_outcome(
                self.client,
                call,
                outcome,
                UpdateRecordOutput(provider_errors=outcome.provider_errors),
                {"requestRejected": "recordRejected"},
            )
        assert response is not None
        receipt = self.client.receipt(call, input.record_id, response.api_usage)
        if response.status == 300:
            return MutationAttempt.branch_result(
                "providerRejected",
                empty,
                receipt,
                failure(
                    "updateRecord",
                    FailureKind.PROTOCOL,
                    "Salesforce returned multiple choices for a record ID",
                ),
            )
        return MutationAttempt.branch_result("updated", UpdateRecordOutput(input.record_id), receipt)


__all__ = [
    "GetRecordOperation",
    "QueryRecordsOperation",
    "UpdateRecordOperation",
    "UpsertRecordByExternalIDOperation",
    "render_soql",
]
