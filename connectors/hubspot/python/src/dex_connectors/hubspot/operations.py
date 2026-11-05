# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""HubSpot CRM operation invokes and Go-compatible provider decoding."""

import json
import re
from datetime import UTC, datetime, timedelta
from typing import TypeVar
from urllib.parse import urlencode, urlsplit

from dex_connectors.sdk import (
    Call,
    CallID,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
)

from ._generated.definitions import (
    GET_OBJECT_DEFINITION,
    SEARCH_OBJECTS_DEFINITION,
    UPDATE_OBJECT_DEFINITION,
    UPSERT_OBJECT_DEFINITION,
)
from .client import Client, Exchange, classify_response, failure
from .models import (
    CRMObject,
    GetObjectInput,
    ObjectPage,
    SearchFilter,
    SearchObjectsInput,
    UpdateObjectInput,
    UpsertedObject,
    UpsertObjectInput,
)

_PROPERTY = re.compile(r"[A-Za-z0-9][A-Za-z0-9_]{0,99}\Z")
_ASSOCIATION = re.compile(r"associations\.[a-z][a-z0-9_]{0,63}\Z")
_RECORD_ID = re.compile(r"[0-9]{1,20}\Z")
_CURSOR = re.compile(r"[0-9]{1,5}\Z")
_DATE = re.compile(r"(\d{4}-\d{2}-\d{2})T(\d{2}:\d{2}:\d{2})(?:[.,](\d+))?(Z|[+-]\d{2}:\d{2})\Z")
_VALUE_OPERATORS = frozenset({"EQ", "NEQ", "LT", "LTE", "GT", "GTE", "CONTAINS_TOKEN", "NOT_CONTAINS_TOKEN"})


def _encode_query(values: dict[str, str]) -> str:
    return urlencode(sorted(values.items()))


def _object_type(value: str) -> None:
    if value not in ("contacts", "companies", "deals"):
        raise ValueError("object type must be contacts, companies, or deals")


def _properties(names: tuple[str, ...]) -> list[str]:
    if len(names) > 100:
        raise ValueError("at most 100 properties can be requested")
    unique: list[str] = []
    for name in names:
        if not _PROPERTY.fullmatch(name):
            raise ValueError("requested property name is invalid")
        if name not in unique:
            unique.append(name)
    return unique


def _property_values(properties: dict[str, str] | None, required: bool) -> None:
    if required and not properties:
        raise ValueError("at least one property value is required")
    if properties is None:
        return
    if len(properties) > 100:
        raise ValueError("at most 100 property values can be written")
    for name, value in properties.items():
        if not _PROPERTY.fullmatch(name):
            raise ValueError("written property name is invalid")
        if len(value) > 65536:
            raise ValueError("property values cannot exceed 65536 characters")


def _filter(value: SearchFilter) -> dict[str, object]:
    if not _PROPERTY.fullmatch(value.property_name) and not _ASSOCIATION.fullmatch(value.property_name):
        raise ValueError("filter property name is invalid")
    has_value, has_high, has_values = bool(value.value), bool(value.high_value), bool(value.values)
    result: dict[str, object] = {"propertyName": value.property_name, "operator": value.operator}
    if value.operator in _VALUE_OPERATORS:
        if not has_value or has_high or has_values:
            raise ValueError(f"filter operator {value.operator} needs only a value")
    elif value.operator == "BETWEEN":
        if not has_value or not has_high or has_values:
            raise ValueError("filter operator BETWEEN needs a value and a highValue")
    elif value.operator in ("IN", "NOT_IN"):
        if has_value or has_high or not 1 <= len(value.values) <= 100:
            raise ValueError(f"filter operator {value.operator} needs 1 to 100 values")
        if any(not item for item in value.values):
            raise ValueError(f"filter operator {value.operator} values must be non-empty")
    elif value.operator in ("HAS_PROPERTY", "NOT_HAS_PROPERTY"):
        if has_value or has_high or has_values:
            raise ValueError(f"filter operator {value.operator} takes no value")
    else:
        raise ValueError("filter operator is not supported")
    if value.value:
        result["value"] = value.value
    if value.high_value:
        result["highValue"] = value.high_value
    if value.values:
        result["values"] = list(value.values)
    return result


def _search_body(value: SearchObjectsInput) -> dict[str, object]:
    _object_type(value.object_type)
    if len(value.filter_groups) > 5:
        raise ValueError("search allows at most 5 filter groups")
    groups = []
    count = 0
    for group in value.filter_groups:
        filters = group["filters"] if isinstance(group, dict) else group.filters
        if not 1 <= len(filters) <= 6:
            raise ValueError("each filter group needs 1 to 6 filters")
        count += len(filters)
        groups.append(
            {
                "filters": [
                    _filter(
                        SearchFilter(
                            property_name=item.get("propertyName", ""),
                            operator=item.get("operator", ""),
                            value=item.get("value", ""),
                            high_value=item.get("highValue", ""),
                            values=tuple(item.get("values", ())),
                        )
                        if isinstance(item, dict)
                        else item
                    )
                    for item in filters
                ]
            }
        )
    if count > 18:
        raise ValueError("search allows at most 18 filters in total")
    properties = _properties(value.properties)
    limit = value.limit or 10
    if not 1 <= limit <= 200:
        raise ValueError("search limit must be from 1 through 200")
    if value.after and not _CURSOR.fullmatch(value.after):
        raise ValueError("search after cursor must be the decimal NextAfter value of a previous page")
    if int(value.after or "0") + limit > 10000:
        raise ValueError("search paging cannot pass HubSpot's 10000-result limit")
    body: dict[str, object] = {"limit": limit}
    if groups:
        body["filterGroups"] = groups
    if value.query:
        body["query"] = value.query
    if value.sort is not None:
        sort_property = (
            value.sort.get("propertyName", "") if isinstance(value.sort, dict) else value.sort.property_name
        )
        sort_direction = (
            value.sort.get("direction", "") if isinstance(value.sort, dict) else value.sort.direction
        )
        if not _PROPERTY.fullmatch(sort_property):
            raise ValueError("sort property name is invalid")
        if sort_direction not in ("ASCENDING", "DESCENDING"):
            raise ValueError("sort direction must be ASCENDING or DESCENDING")
        body["sorts"] = [{"propertyName": sort_property, "direction": sort_direction}]
    if properties:
        body["properties"] = properties
    if value.after:
        body["after"] = value.after
    if len(json.dumps(body, ensure_ascii=False, separators=(",", ":")).encode()) > 3000:
        raise ValueError("search request exceeds HubSpot's 3000-character body limit")
    return body


def _record_id(value: str) -> None:
    if not _RECORD_ID.fullmatch(value):
        raise ValueError("object ID must be a decimal HubSpot record ID")


def _mapping(value: object) -> dict[str, object]:
    if not isinstance(value, dict):
        raise ValueError("HubSpot returned a malformed response")
    return value


def _text(value: object, default: str = "") -> str:
    if value is None:
        return default
    if not isinstance(value, str):
        raise ValueError("HubSpot returned a malformed response")
    return value


def _boolean(value: object) -> bool:
    if value is None:
        return False
    if type(value) is not bool:
        raise ValueError("HubSpot returned a malformed response")
    return value


def _date(value: object) -> datetime:
    if value is None:
        return datetime.min.replace(tzinfo=UTC)
    raw = _text(value)
    if not raw:
        return datetime.min.replace(tzinfo=UTC)
    match = _DATE.fullmatch(raw)
    if match is None:
        raise ValueError("HubSpot returned a malformed response")
    fraction = (match.group(3) or "")[:6].ljust(6, "0")
    base = datetime.fromisoformat(f"{match.group(1)}T{match.group(2)}.{fraction}+00:00")
    zone = match.group(4)
    if zone != "Z":
        hours, minutes = int(zone[1:3]), int(zone[4:])
        if minutes > 59:
            raise ValueError("HubSpot returned a malformed response")
        offset = timedelta(hours=hours, minutes=minutes)
        base += -offset if zone[0] == "+" else offset
    return base


def _crm_object(object_type: str, raw: object) -> CRMObject:
    value = _mapping(raw)
    record_id = _text(value.get("id"))
    if not _RECORD_ID.fullmatch(record_id):
        raise ValueError("HubSpot returned a record without a valid ID")
    properties_raw = value.get("properties")
    properties = {}
    if properties_raw is not None:
        for name, item in _mapping(properties_raw).items():
            if item is not None:
                properties[name] = _text(item)
    url = _text(value.get("url"))
    parsed = urlsplit(url)
    if (
        parsed.scheme != "https"
        or parsed.username is not None
        or not (parsed.hostname or "").endswith(".hubspot.com")
    ):
        url = ""
    return CRMObject(
        object_type,
        record_id,
        properties,
        _date(value.get("createdAt")),
        _date(value.get("updatedAt")),
        _boolean(value.get("archived")),
        url,
    )


def _page(object_type: str, body: bytes) -> ObjectPage:
    value = _mapping(json.loads(body))
    total = value.get("total", 0)
    if total is None:
        total = 0
    if type(total) is not int or total < 0:
        raise ValueError("HubSpot returned a malformed response")
    results = value.get("results") or []
    if not isinstance(results, list):
        raise ValueError("HubSpot returned a malformed response")
    next_after = ""
    if value.get("paging") is not None:
        next_value = _mapping(value["paging"]).get("next")
        if next_value is not None:
            next_after = _text(_mapping(next_value).get("after"))
            if next_after and not _CURSOR.fullmatch(next_after):
                raise ValueError("HubSpot returned an invalid search cursor")
    return ObjectPage(
        object_type, total, tuple(_crm_object(object_type, item) for item in results), next_after
    )


Output = TypeVar("Output")


def _outcome_branch(operation: str, disposition: str) -> str:
    if disposition == "notFound" and operation in ("searchObjects", "upsertObject"):
        return "providerRejected"
    if disposition == "conflict" and operation in ("searchObjects", "getObject"):
        return "providerRejected"
    return "providerRejected" if disposition == "rejected" else disposition


def _query_outcome(
    client: Client, call: Call, operation: str, result: Exchange, empty: Output, object_id: str = ""
) -> QueryAttempt[Output]:
    if result.disposition == "retry":
        assert result.failure is not None
        return QueryAttempt.retry(result.failure, result.retry_after)
    return QueryAttempt.branch_result(
        _outcome_branch(operation, result.disposition),
        empty,
        client.receipt(call, result, object_id),
        result.failure,
    )


def _mutation_outcome(
    client: Client, call: Call, operation: str, result: Exchange, empty: Output, object_id: str = ""
) -> MutationAttempt[Output]:
    if result.disposition == "retry":
        assert result.failure is not None
        return MutationAttempt.retry(result.failure, result.retry_after)
    return MutationAttempt.branch_result(
        _outcome_branch(operation, result.disposition),
        empty,
        client.receipt(call, result, object_id),
        result.failure,
    )


class SearchObjectsOperation:
    """Search one bounded CRM page per Dex Query attempt."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return SEARCH_OBJECTS_DEFINITION

    async def invoke(self, call: Call, input: SearchObjectsInput) -> QueryAttempt[ObjectPage]:
        """Validate a search and decode the provider's page."""
        empty = ObjectPage("")
        try:
            body = _search_body(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                self.client.receipt(call),
                failure("searchObjects", FailureKind.VALIDATION, str(error)),
            )
        result = await self.client.exchange(
            call, "searchObjects", "POST", f"crm/objects/2026-09/{input.object_type}/search", body=body
        )
        if result.disposition:
            return _query_outcome(self.client, call, "searchObjects", result, empty)
        try:
            page = _page(input.object_type, result.body)
        except (ValueError, TypeError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, result),
                failure("searchObjects", FailureKind.PROTOCOL, "HubSpot returned a malformed response"),
            )
        return QueryAttempt.branch_result("searched", page, self.client.receipt(call, result))


class GetObjectOperation:
    """Read one unarchived CRM record per Dex Query attempt."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return GET_OBJECT_DEFINITION

    async def invoke(self, call: Call, input: GetObjectInput) -> QueryAttempt[CRMObject]:
        """Validate record identity and decode a HubSpot record."""
        empty = CRMObject("")
        try:
            _object_type(input.object_type)
            _record_id(input.object_id)
            properties = _properties(input.properties)
        except ValueError as error:
            return QueryAttempt.branch_result(
                "defect",
                empty,
                self.client.receipt(call),
                failure("getObject", FailureKind.VALIDATION, str(error)),
            )
        query = _encode_query(
            {"archived": "false", **({"properties": ",".join(properties)} if properties else {})}
        )
        result = await self.client.exchange(
            call, "getObject", "GET", f"crm/objects/2026-09/{input.object_type}/{input.object_id}", query
        )
        if result.disposition:
            return _query_outcome(self.client, call, "getObject", result, empty, input.object_id)
        try:
            value = _crm_object(input.object_type, json.loads(result.body))
            if value.id != input.object_id:
                raise ValueError("record ID mismatch")
        except (ValueError, TypeError, OverflowError):
            return QueryAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, result, input.object_id),
                failure("getObject", FailureKind.PROTOCOL, "HubSpot returned an invalid record"),
            )
        return QueryAttempt.branch_result("found", value, self.client.receipt(call, result, input.object_id))


class UpsertObjectOperation:
    """Upsert a CRM record by a unique property."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return UPSERT_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertObjectInput) -> IdempotencyKey:
        """Reuse the stable Call ID; HubSpot deduplicates by the unique property."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpsertObjectInput) -> MutationAttempt[UpsertedObject]:
        """Send at most two exchanges when a concurrent create conflicts."""
        empty = UpsertedObject(CRMObject(""))
        try:
            _object_type(input.object_type)
            if not _PROPERTY.fullmatch(input.id_property):
                raise ValueError("upsert ID property name is invalid")
            if not input.id_value.strip() or len(input.id_value) > 65536:
                raise ValueError(
                    "upsert ID value must be non-empty and within HubSpot's property value limit"
                )
            _property_values(input.properties, False)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self.client.receipt(call),
                failure("upsertObject", FailureKind.VALIDATION, str(error)),
            )
        body = {
            "inputs": [
                {"idProperty": input.id_property, "id": input.id_value, "properties": input.properties or {}}
            ]
        }
        for dispatch in range(2):
            result = await self.client.exchange(
                call,
                "upsertObject",
                "POST",
                f"crm/objects/2026-09/{input.object_type}/batch/upsert",
                body=body,
            )
            if not result.disposition:
                try:
                    decoded = _mapping(json.loads(result.body))
                    records = decoded.get("results") or []
                    errors = decoded.get("errors") or []
                    if not isinstance(records, list) or not isinstance(errors, list):
                        raise ValueError("HubSpot returned a malformed response")
                    if not records and errors:
                        first = _mapping(errors[0])
                        status_text = _text(first.get("status"))
                        status = (
                            int(status_text)
                            if re.fullmatch(r"[+-]?\d+", status_text) and int(status_text) >= 400
                            else 400
                        )
                        category = _text(first.get("category"))
                        result = classify_response(
                            "upsertObject",
                            Exchange(status, result.headers, json.dumps({"category": category}).encode()),
                            self.client.now(),
                        )
                    elif len(records) != 1:
                        raise ValueError(
                            "HubSpot upsert response has no record"
                            if not records
                            else "HubSpot upsert response has more than one record"
                        )
                    else:
                        record = _mapping(records[0])
                        value = _crm_object(input.object_type, record)
                        created = _boolean(record.get("new"))
                        return MutationAttempt.branch_result(
                            "upserted",
                            UpsertedObject(value, created),
                            self.client.receipt(call, result, value.id),
                        )
                except (ValueError, TypeError, OverflowError) as error:
                    return MutationAttempt.branch_result(
                        "invalidResponse",
                        empty,
                        self.client.receipt(call, result),
                        failure("upsertObject", FailureKind.PROTOCOL, str(error)),
                    )
            if result.disposition == "conflict" and dispatch == 0:
                continue
            return _mutation_outcome(self.client, call, "upsertObject", result, empty)
        raise AssertionError("unreachable upsert dispatch")


class UpdateObjectOperation:
    """Replace named CRM property values on a record."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated Mutation definition."""
        return UPDATE_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateObjectInput) -> IdempotencyKey:
        """Reuse the stable Call ID for an absolute property replacement."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateObjectInput) -> MutationAttempt[CRMObject]:
        """Validate, replace, and decode the updated CRM record."""
        empty = CRMObject("")
        try:
            _object_type(input.object_type)
            _record_id(input.object_id)
            _property_values(input.properties, True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                "defect",
                empty,
                self.client.receipt(call),
                failure("updateObject", FailureKind.VALIDATION, str(error)),
            )
        result = await self.client.exchange(
            call,
            "updateObject",
            "PATCH",
            f"crm/objects/2026-09/{input.object_type}/{input.object_id}",
            body={"properties": input.properties},
        )
        if result.disposition:
            return _mutation_outcome(self.client, call, "updateObject", result, empty, input.object_id)
        try:
            value = _crm_object(input.object_type, json.loads(result.body))
            if value.id != input.object_id:
                raise ValueError("record ID mismatch")
        except (ValueError, TypeError, OverflowError):
            return MutationAttempt.branch_result(
                "invalidResponse",
                empty,
                self.client.receipt(call, result, input.object_id),
                failure("updateObject", FailureKind.PROTOCOL, "HubSpot returned an invalid record"),
            )
        return MutationAttempt.branch_result(
            "updated", value, self.client.receipt(call, result, input.object_id)
        )
