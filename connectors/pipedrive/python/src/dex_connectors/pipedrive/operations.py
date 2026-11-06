# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pipedrive Query and Mutation invocation with Go-compatible provider outcomes."""

import json
import re
from datetime import datetime, timedelta
from email._header_value_parser import get_addr_spec
from typing import Any, cast
from urllib.parse import urlencode

from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    Receipt,
)
from dex_connectors.sdk.singledispatch import (
    CheckpointError,
    DecisionKind,
    Fence,
    InsufficientAttemptTimeError,
)

from ._generated import definitions as definitions
from .client import Client, Exchange, failure
from .models import (
    CreateObjectInput,
    CRMObject,
    GetObjectInput,
    ListObjectsInput,
    ObjectPage,
    SearchObjectsInput,
    UpdateObjectInput,
    UpsertedObject,
    UpsertObjectInput,
    decode_page,
    decode_record_body,
    parse_go_rfc3339,
    validate_cursor,
    validate_limit,
    validate_object_type,
    validate_record_id,
)

_SEARCH_FIELDS = {
    "persons": ("name", "email", "phone", "notes", "custom_fields"),
    "organizations": ("name", "address", "notes", "custom_fields"),
    "deals": ("title", "notes", "custom_fields"),
}
_CUSTOM_FIELD = re.compile(r"[0-9a-f]{40}\Z")
_STANDARD_FIELD = re.compile(r"[a-z][a-z0-9_]{0,63}\Z")


def _equal_fold(left: str, right: str) -> bool:
    return len(left) == len(right) and all(
        first == second
        or (len(first.lower()) == len(second.lower()) == 1 and first.lower() == second.lower())
        or (len(first.upper()) == len(second.upper()) == 1 and first.upper() == second.upper())
        or (len(first.casefold()) == len(second.casefold()) == 1 and first.casefold() == second.casefold())
        for first, second in zip(left, right, strict=True)
    )


def _encode_query(values: dict[str, str]) -> str:
    """Encode sorted query pairs like Go's url.Values.Encode."""
    return urlencode(sorted(values.items()))


def _statuses(values: tuple[str, ...], allow_deleted: bool) -> str:
    names: list[str] = []
    for status in values:
        if status not in (("open", "won", "lost", "deleted") if allow_deleted else ("open", "won", "lost")):
            raise ValueError(f'deal status "{status}" is not supported here')
        if status not in names:
            names.append(status)
    return ",".join(names)


def _search_query(input: SearchObjectsInput) -> str:
    validate_object_type(input.object_type)
    minimum = 1 if input.exact_match else 2
    if len(input.term.strip()) < minimum or len(input.term) > 255:
        raise ValueError(f"search term needs {minimum} to 255 characters")
    query = {"term": input.term, "limit": str(validate_limit(input.limit))}
    if input.fields:
        names: list[str] = []
        for field in input.fields:
            if field not in _SEARCH_FIELDS[input.object_type]:
                raise ValueError(f'search field "{field}" is not searchable for {input.object_type}')
            if field not in names:
                names.append(field)
        query["fields"] = ",".join(names)
    if input.exact_match:
        query["exact_match"] = "true"
    if input.organization_id:
        if input.object_type == "organizations":
            raise ValueError("organizationId filters only persons and deals searches")
        validate_record_id("organizationId", input.organization_id)
        query["organization_id"] = input.organization_id
    if input.object_type == "deals":
        if input.person_id:
            validate_record_id("personId", input.person_id)
            query["person_id"] = input.person_id
        statuses = _statuses(input.statuses, False)
        if statuses:
            query["status"] = statuses
    elif input.person_id or input.statuses:
        raise ValueError("personId and statuses filter only deals searches")
    validate_cursor(input.cursor)
    if input.cursor:
        query["cursor"] = input.cursor
    return _encode_query(query)


def _go_time(value: str) -> str:
    """Render whole-second UTC times used in Pipedrive filters."""
    instant, _ = parse_go_rfc3339(value, is_go_json_time=True)
    return instant.strftime("%Y-%m-%dT%H:%M:%SZ")


def _list_query(input: ListObjectsInput) -> str:
    validate_object_type(input.object_type)
    query = {"limit": str(validate_limit(input.limit))}
    zero = "0001-01-01T00:00:00Z"
    if input.updated_since != zero:
        query["updated_since"] = _go_time(input.updated_since)
    if input.updated_until != zero:
        if input.updated_since != zero and parse_go_rfc3339(
            input.updated_until, is_go_json_time=True
        ) <= parse_go_rfc3339(
            input.updated_since, is_go_json_time=True
        ):
            raise ValueError("updatedUntil must be after updatedSince")
        query["updated_until"] = _go_time(input.updated_until)
    is_deals = input.object_type == "deals"
    for name, query_name, value, supported in (
        ("ownerId", "owner_id", input.owner_id, True),
        ("organizationId", "org_id", input.organization_id, input.object_type != "organizations"),
        ("personId", "person_id", input.person_id, is_deals),
        ("pipelineId", "pipeline_id", input.pipeline_id, is_deals),
        ("stageId", "stage_id", input.stage_id, is_deals),
    ):
        if value:
            if not supported:
                raise ValueError(f"{name} does not filter {input.object_type}")
            validate_record_id(name, value)
            query[query_name] = value
    if input.statuses and not is_deals:
        raise ValueError("statuses filter only deals")
    statuses = _statuses(input.statuses, True)
    if statuses:
        query["status"] = statuses
    sort_by = input.sort_by or ("update_time" if input.updated_since != zero else "")
    if sort_by:
        if sort_by not in ("id", "update_time", "add_time"):
            raise ValueError("sortBy must be id, update_time, or add_time")
        query["sort_by"] = str(sort_by)
    if input.sort_direction:
        if input.sort_direction not in ("asc", "desc"):
            raise ValueError("sortDirection must be asc or desc")
        query["sort_direction"] = input.sort_direction
    if len(input.custom_field_keys) > 15:
        raise ValueError("at most 15 custom field keys can be requested")
    for key in input.custom_field_keys:
        if not _CUSTOM_FIELD.fullmatch(key):
            raise ValueError("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key")
    if input.custom_field_keys:
        query["custom_fields"] = ",".join(input.custom_field_keys)
    validate_cursor(input.cursor)
    if input.cursor:
        query["cursor"] = input.cursor
    return _encode_query(query)


def _write_body(
    fields: dict[str, Any], custom_fields: dict[str, Any], object_type: str, *, require_name: bool = False
) -> bytes:
    validate_object_type(object_type)
    if len(fields) + len(custom_fields) > 100:
        raise ValueError("at most 100 field values can be written")

    def encoded_length(value: Any) -> int:
        try:
            return len(json.dumps(value, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode())
        except (TypeError, ValueError):
            raise ValueError("value must be one JSON value") from None

    for name, value in fields.items():
        if not _STANDARD_FIELD.fullmatch(name) or name in ("id", "custom_fields"):
            raise ValueError("a written field name is invalid; put custom fields in customFields")
        if encoded_length(value) > 65536:
            raise ValueError(f"field {name} value exceeds 65536 bytes")
    for key, value in custom_fields.items():
        if not _CUSTOM_FIELD.fullmatch(key):
            raise ValueError("a custom field key must be Pipedrive's 40-character lowercase hexadecimal key")
        if encoded_length(value) > 65536:
            raise ValueError(f"custom field {key} value exceeds 65536 bytes")
    if require_name:
        name_field = "title" if object_type == "deals" else "name"
        record_name = fields.get(name_field)
        if not isinstance(record_name, str) or not record_name.strip():
            raise ValueError(f"field {name_field} must be a non-blank JSON string")
    body = dict(fields)
    if custom_fields:
        body["custom_fields"] = custom_fields
    return json.dumps(
        body, ensure_ascii=False, separators=(",", ":"), sort_keys=True, allow_nan=False
    ).encode()


def _query_result(
    result: Exchange, receipt: Receipt, empty: Any, *, not_found: bool = False
) -> QueryAttempt[Any]:
    if result.outcome in ("retry", "notSent", "rateLimited", "unavailable"):
        return QueryAttempt.retry(cast(Failure, result.failure), result.retry_after)
    branch = (
        definitions.NOT_FOUND
        if result.outcome == "notFound" and not_found
        else definitions.INVALID_RESPONSE
        if result.outcome == "invalid"
        else definitions.PROVIDER_REJECTED
        if result.outcome in ("notFound", "rejected")
        else definitions.DEFECT
    )
    return QueryAttempt.branch_result(branch, empty, receipt, result.failure)


def _mutation_result(
    result: Exchange, receipt: Receipt, empty: Any, *, not_found: bool = False
) -> MutationAttempt[Any]:
    if result.outcome in ("retry", "notSent", "rateLimited", "unavailable"):
        return MutationAttempt.retry(cast(Failure, result.failure), result.retry_after)
    branch = (
        definitions.NOT_FOUND
        if result.outcome == "notFound" and not_found
        else definitions.INVALID_RESPONSE
        if result.outcome == "invalid"
        else definitions.PROVIDER_REJECTED
        if result.outcome in ("notFound", "rejected")
        else definitions.DEFECT
    )
    return MutationAttempt.branch_result(branch, empty, receipt, result.failure)


class SearchObjectsOperation:
    """Search one bounded page of Pipedrive records."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated search contract."""
        return definitions.SEARCH_OBJECTS_DEFINITION

    async def invoke(self, call: Call, input: SearchObjectsInput) -> QueryAttempt[ObjectPage]:
        """Search the provider and decode partial records."""
        operation = "searchObjects"
        try:
            query = _search_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                ObjectPage(""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            return _query_result(issue, Receipt(), ObjectPage(""))
        assert credentials is not None
        result = await self.client.exchange(
            call, operation, credentials, "GET", f"/api/v2/{input.object_type}/search", query=query
        )
        receipt = self.client.receipt(call, result)
        if result.outcome != "succeeded":
            return _query_result(result, receipt, ObjectPage(""))
        try:
            page = decode_page(input.object_type, result.body, search=True)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                ObjectPage(""),
                receipt,
                failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result(definitions.SEARCHED, page, receipt)


class GetObjectOperation:
    """Read one complete Pipedrive record."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated get contract."""
        return definitions.GET_OBJECT_DEFINITION

    async def invoke(self, call: Call, input: GetObjectInput) -> QueryAttempt[CRMObject]:
        """Read the selected provider record."""
        operation = "getObject"
        try:
            validate_object_type(input.object_type)
            validate_record_id("objectId", input.object_id)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                CRMObject(""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            return _query_result(issue, Receipt(), CRMObject(""), not_found=True)
        assert credentials is not None
        result = await self.client.exchange(
            call, operation, credentials, "GET", f"/api/v2/{input.object_type}/{input.object_id}"
        )
        receipt = self.client.receipt(call, result, input.object_id)
        if result.outcome != "succeeded":
            return _query_result(result, receipt, CRMObject(""), not_found=True)
        try:
            record = decode_record_body(input.object_type, result.body)
            if record.id != input.object_id:
                raise ValueError("Pipedrive returned an invalid record")
        except ValueError:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                CRMObject(""),
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Pipedrive returned an invalid record"),
            )
        return QueryAttempt.branch_result(definitions.FOUND, record, receipt)


class ListObjectsOperation:
    """List one bounded page of full Pipedrive records."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated list contract."""
        return definitions.LIST_OBJECTS_DEFINITION

    async def invoke(self, call: Call, input: ListObjectsInput) -> QueryAttempt[ObjectPage]:
        """List the provider records matching the input filters."""
        operation = "listObjects"
        try:
            query = _list_query(input)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.DEFECT,
                ObjectPage(""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            return _query_result(issue, Receipt(), ObjectPage(""))
        assert credentials is not None
        result = await self.client.exchange(
            call, operation, credentials, "GET", f"/api/v2/{input.object_type}", query=query
        )
        receipt = self.client.receipt(call, result)
        if result.outcome != "succeeded":
            return _query_result(result, receipt, ObjectPage(""))
        try:
            page = decode_page(input.object_type, result.body, search=False)
        except ValueError as error:
            return QueryAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                ObjectPage(""),
                receipt,
                failure(operation, FailureKind.PROTOCOL, str(error)),
            )
        return QueryAttempt.branch_result(definitions.LISTED, page, receipt)


class UpdateObjectOperation:
    """Set named fields on one Pipedrive record with repeatable PATCH."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated update contract."""
        return definitions.UPDATE_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpdateObjectInput) -> IdempotencyKey:
        """Correlate a repeatable PATCH through its stable Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: UpdateObjectInput) -> MutationAttempt[CRMObject]:
        """Patch one record and classify its response."""
        operation = "updateObject"
        try:
            validate_record_id("objectId", input.object_id)
            if not input.fields and not input.custom_fields:
                raise ValueError("at least one field or custom field value is required")
            body = _write_body(input.fields, input.custom_fields, input.object_type)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                CRMObject(""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            return _mutation_result(issue, Receipt(), CRMObject(""), not_found=True)
        assert credentials is not None
        result = await self.client.exchange(
            call, operation, credentials, "PATCH", f"/api/v2/{input.object_type}/{input.object_id}", body=body
        )
        receipt = self.client.receipt(call, result, input.object_id)
        if result.outcome != "succeeded":
            return _mutation_result(result, receipt, CRMObject(""), not_found=True)
        try:
            record = decode_record_body(input.object_type, result.body)
            if record.id != input.object_id:
                raise ValueError("Pipedrive returned an invalid record")
        except ValueError:
            return MutationAttempt.branch_result(
                definitions.INVALID_RESPONSE,
                CRMObject(""),
                receipt,
                failure(operation, FailureKind.PROTOCOL, "Pipedrive returned an invalid record"),
            )
        return MutationAttempt.branch_result(definitions.UPDATED, record, receipt)


def _uncertain(
    operation: str, message: str, receipt: Receipt, kind: FailureKind = FailureKind.TRANSPORT
) -> MutationAttempt[Any]:
    return MutationAttempt.uncertain(
        UpsertedObject(CRMObject("")) if operation == "upsertObject" else CRMObject(""),
        failure(operation, kind, "this Step sent a create that was never confirmed, and " + message)
        if operation == "upsertObject"
        else failure(operation, kind, message),
        receipt,
    )


async def _record_can_resend(fence: Fence, operation: str) -> MutationAttempt[Any] | None:
    try:
        await fence.record_can_resend()
        return None
    except Exception:
        return MutationAttempt.retry(
            failure(
                operation,
                FailureKind.AVAILABILITY,
                (
                    "The single-dispatch fence could not authorize a send or retry; "
                    "the next attempt must reconcile the checkpoint"
                ),
            )
        )


class CreateObjectOperation:
    """Create a Pipedrive record once through Dex's durable dispatch fence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated create contract."""
        return definitions.CREATE_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateObjectInput) -> IdempotencyKey:
        """Correlate a create through its stable Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: CreateObjectInput) -> MutationAttempt[CRMObject]:
        """Send one create or read back a prior accepted provider ID."""
        operation = "createObject"
        try:
            fence = Fence(call, timedelta(seconds=30))
            decision = await fence.decide()
        except ValueError:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                CRMObject(""),
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "single-dispatch fence could not be built"),
            )
        if decision.kind != DecisionKind.SEND_ALLOWED:
            marker = decision.marker
            provider_id = marker.provider_id if marker and marker.call_id == call.id else ""
            if decision.kind == DecisionKind.READ_BACK_REQUIRED and provider_id:
                credentials, issue = await self.client.resolve(call, operation)
                if issue:
                    if issue.outcome == "retry":
                        return MutationAttempt.retry(cast(Failure, issue.failure))
                    return _uncertain(
                        operation,
                        cast(Failure, issue.failure).message,
                        self.client.receipt(call, Exchange(""), provider_id),
                        cast(Failure, issue.failure).kind,
                    )
                assert credentials is not None
                result = await self.client.exchange(
                    call, operation, credentials, "GET", f"/api/v2/{input.object_type}/{provider_id}"
                )
                receipt = self.client.receipt(call, result, provider_id)
                if result.outcome in ("notSent", "rateLimited", "unavailable"):
                    return MutationAttempt.retry(cast(Failure, result.failure), result.retry_after)
                if result.outcome == "succeeded":
                    try:
                        record = decode_record_body(input.object_type, result.body)
                        if record.id != provider_id:
                            raise ValueError("mismatch")
                        return MutationAttempt.branch_result(
                            definitions.CREATED, record, self.client.receipt(call, result, record.id)
                        )
                    except ValueError:
                        pass
                return _uncertain(
                    operation,
                    result.failure.message if result.failure else "Pipedrive returned an invalid record",
                    receipt,
                    result.failure.kind if result.failure else FailureKind.PROTOCOL,
                )
            return _uncertain(
                operation,
                "an earlier create may have reached Pipedrive and cannot be confirmed",
                self.client.receipt(call, Exchange("")),
            )
        try:
            body = _write_body(input.fields or {}, input.custom_fields, input.object_type, require_name=True)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                CRMObject(""),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(
                    failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        (
                            "The single-dispatch fence could not authorize a send or retry; "
                            "the next attempt must reconcile the checkpoint"
                        ),
                    )
                )
            return _mutation_result(issue, Receipt(), CRMObject(""))
        assert credentials is not None

        async def send(deadline: datetime) -> Exchange:
            return await self.client.exchange(
                call,
                operation,
                credentials,
                "POST",
                f"/api/v2/{input.object_type}",
                body=body,
                allow_refresh=False,
            )

        try:
            result = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    (
                        "The single-dispatch fence could not authorize a send or retry; "
                        "the next attempt must reconcile the checkpoint"
                    ),
                )
            )
        except Exception:
            return _uncertain(operation, "create dispatch could not be confirmed", Receipt())
        receipt = self.client.receipt(call, result)
        if result.status == 401 and credentials.auth_method_id == "pipedrive-oauth":
            refreshed, rejected_failure = await self.client.refresh_after_write_rejection(
                call, credentials, operation
            )
            if refreshed:
                problem = await _record_can_resend(fence, operation)
                return problem or MutationAttempt.retry(cast(Failure, result.failure))
            result.failure = rejected_failure
        if (
            result.outcome in ("notSent", "rateLimited")
            or result.outcome == "unavailable"
            and result.status == 503
        ):
            problem = await _record_can_resend(fence, operation)
            return problem or MutationAttempt.retry(cast(Failure, result.failure), result.retry_after)
        if result.outcome in ("unavailable", "invalid"):
            return MutationAttempt.uncertain(CRMObject(""), cast(Failure, result.failure), receipt)
        if result.outcome == "defect":
            return MutationAttempt.branch_result(definitions.DEFECT, CRMObject(""), receipt, result.failure)
        if result.outcome != "succeeded":
            return MutationAttempt.branch_result(
                definitions.PROVIDER_REJECTED, CRMObject(""), receipt, result.failure
            )
        try:
            record = decode_record_body(input.object_type, result.body)
        except ValueError:
            return MutationAttempt.uncertain(
                CRMObject(""),
                failure(
                    operation,
                    FailureKind.PROTOCOL,
                    "Pipedrive accepted the create but returned an invalid record",
                ),
                receipt,
            )
        try:
            await fence.record_provider_id(record.id)
        except Exception:
            pass  # The accepted response still proves the create even if the extra checkpoint fails.
        return MutationAttempt.branch_result(
            definitions.CREATED, record, self.client.receipt(call, result, record.id)
        )


def _upsert_bodies(input: UpsertObjectInput) -> tuple[str, bytes, bytes | None]:
    _write_body(input.fields, input.custom_fields, "persons")
    create_fields = dict(input.fields)
    if input.object_type == "persons":
        if input.id_property != "email":
            raise ValueError("a persons upsert is identified by idProperty email")
        address = input.id_value
        try:
            parsed, remainder = get_addr_spec(address)
            is_valid = (
                not remainder
                and not parsed.defects
                and parsed.addr_spec == address
                and len(address.encode()) <= 254
                and ".." not in address.split("@", 1)[0]
            )
        except (ValueError, IndexError):
            is_valid = False
        if not is_valid:
            raise ValueError("idValue must be one plain email address")
        if "emails" in input.fields:
            raise ValueError("fields cannot set emails; the connector writes idValue as the primary email")
        name = input.fields.get("name")
        if not isinstance(name, str) or not name.strip():
            raise ValueError("field name must be a non-blank JSON string")
        create_fields["emails"] = [{"value": address, "primary": True, "label": "work"}]
        identity_field = "email"
    elif input.object_type == "organizations":
        if input.id_property != "name":
            raise ValueError("an organizations upsert is identified by idProperty name")
        if not input.id_value or input.id_value.strip() != input.id_value or len(input.id_value) > 255:
            raise ValueError("idValue must be a name of 1 to 255 characters without surrounding spaces")
        if "name" in input.fields:
            raise ValueError("fields cannot set name; the connector writes idValue as the organization name")
        create_fields["name"] = input.id_value
        identity_field = "name"
    else:
        raise ValueError(
            "upsert supports persons by email and organizations by name; create deals with createObject"
        )
    create_body = _write_body(create_fields, input.custom_fields, input.object_type)
    update_body = (
        _write_body(input.fields, input.custom_fields, input.object_type)
        if input.fields or input.custom_fields
        else None
    )
    return identity_field, create_body, update_body


def _upsert_empty() -> UpsertedObject:
    return UpsertedObject(CRMObject(""))


class UpsertObjectOperation:
    """Search an identity and update or fence a single Pipedrive create."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the generated upsert contract."""
        return definitions.UPSERT_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: UpsertObjectInput) -> IdempotencyKey:
        """Correlate identity reconciliation through the stable Dex Call ID."""
        return IdempotencyKey(str(call_id))

    async def _find_matches(
        self, call: Call, input: UpsertObjectInput, credentials: Any, identity_field: str
    ) -> tuple[list[str], bool, Exchange]:
        query = _encode_query(
            {"term": input.id_value, "fields": identity_field, "exact_match": "true", "limit": "50"}
        )
        result = await self.client.exchange(
            call, "upsertObject", credentials, "GET", f"/api/v2/{input.object_type}/search", query=query
        )
        if result.outcome != "succeeded":
            return [], False, result
        try:
            page = decode_page(input.object_type, result.body, search=True)
        except ValueError as error:
            result.outcome = "invalid"
            result.failure = failure("upsertObject", FailureKind.PROTOCOL, str(error))
            return [], False, result
        matches = []
        for record in page.objects:
            if input.object_type == "organizations":
                found = _equal_fold(record.name.strip(), input.id_value)
            else:
                found = any(_equal_fold(email.strip(), input.id_value) for email in record.emails)
            if found:
                matches.append(record.id)
        return matches, bool(page.next_cursor), result

    async def _read_or_update(
        self,
        call: Call,
        input: UpsertObjectInput,
        credentials: Any,
        object_id: str,
        update_body: bytes | None,
        earlier_create: bool,
        fence: Fence,
    ) -> MutationAttempt[UpsertedObject]:
        method = "PATCH" if update_body is not None else "GET"
        result = await self.client.exchange(
            call,
            "upsertObject",
            credentials,
            method,
            f"/api/v2/{input.object_type}/{object_id}",
            body=update_body,
        )
        receipt = self.client.receipt(call, result, object_id)
        if result.outcome == "succeeded":
            try:
                record = decode_record_body(input.object_type, result.body)
                if record.id != object_id:
                    raise ValueError("mismatch")
            except ValueError:
                result.outcome = "invalid"
                result.failure = failure(
                    "upsertObject", FailureKind.PROTOCOL, "Pipedrive returned an invalid record"
                )
            else:
                return MutationAttempt.branch_result(definitions.UPSERTED, UpsertedObject(record), receipt)
        if earlier_create and result.outcome not in ("notSent", "rateLimited", "unavailable"):
            return _uncertain(
                "upsertObject",
                cast(Failure, result.failure).message,
                receipt,
                cast(Failure, result.failure).kind,
            )
        if not earlier_create and result.outcome in ("notSent", "rateLimited", "unavailable"):
            try:
                await fence.record_not_sent()
            except Exception:
                return MutationAttempt.retry(failure(
                    "upsertObject", FailureKind.AVAILABILITY,
                    (
                        "The single-dispatch fence could not authorize a send or retry; "
                        "the next attempt must reconcile the checkpoint"
                    ),
                ))
        return _mutation_result(result, receipt, _upsert_empty())

    async def invoke(self, call: Call, input: UpsertObjectInput) -> MutationAttempt[UpsertedObject]:
        """Reconcile identity and send at most one provider create."""
        operation = "upsertObject"
        try:
            fence = Fence(call, timedelta(seconds=30))
            decision = await fence.decide()
        except ValueError:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                _upsert_empty(),
                Receipt(),
                failure(operation, FailureKind.LOCAL_DEFECT, "single-dispatch fence could not be built"),
            )
        if decision.kind == DecisionKind.FAIL_CLOSED:
            return _uncertain(operation, "create checkpoint could not be read", Receipt())
        try:
            identity_field, create_body, update_body = _upsert_bodies(input)
        except ValueError as error:
            return MutationAttempt.branch_result(
                definitions.DEFECT,
                _upsert_empty(),
                Receipt(),
                failure(operation, FailureKind.VALIDATION, str(error)),
            )
        earlier_create = decision.kind == DecisionKind.READ_BACK_REQUIRED
        credentials, issue = await self.client.resolve(call, operation)
        if issue:
            if not earlier_create:
                try:
                    await fence.record_not_sent()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            (
                                "The single-dispatch fence could not authorize a send or retry; "
                                "the next attempt must reconcile the checkpoint"
                            ),
                        )
                    )
                return _mutation_result(issue, Receipt(), _upsert_empty())
            if issue.outcome == "retry":
                return MutationAttempt.retry(cast(Failure, issue.failure))
            marker_id = (
                decision.marker.provider_id if decision.marker and decision.marker.call_id == call.id else ""
            )
            return _uncertain(
                operation,
                cast(Failure, issue.failure).message,
                self.client.receipt(call, Exchange(""), marker_id),
                cast(Failure, issue.failure).kind,
            )
        assert credentials is not None
        marker_id = (
            decision.marker.provider_id if decision.marker and decision.marker.call_id == call.id else ""
        )
        if earlier_create and marker_id:
            read = await self._read_or_update(call, input, credentials, marker_id, None, True, fence)
            if read.kind != "branch" or read.branch != definitions.UPSERTED or update_body is None:
                return read
            return await self._read_or_update(call, input, credentials, marker_id, update_body, True, fence)
        matches, has_more, result = await self._find_matches(call, input, credentials, identity_field)
        receipt = self.client.receipt(call, result)
        if result.outcome != "succeeded":
            if earlier_create and result.outcome not in ("notSent", "rateLimited", "unavailable"):
                return _uncertain(
                    operation,
                    cast(Failure, result.failure).message,
                    receipt,
                    cast(Failure, result.failure).kind,
                )
            if not earlier_create and result.outcome in ("notSent", "rateLimited", "unavailable"):
                try:
                    await fence.record_not_sent()
                except Exception:
                    return MutationAttempt.retry(
                        failure(
                            operation,
                            FailureKind.AVAILABILITY,
                            (
                                "The single-dispatch fence could not authorize a send or retry; "
                                "the next attempt must reconcile the checkpoint"
                            ),
                        )
                    )
            return _mutation_result(result, receipt, _upsert_empty())
        if len(matches) > 1 or has_more:
            return MutationAttempt.branch_result(
                definitions.MULTIPLE_MATCHES,
                UpsertedObject(CRMObject(""), matching_ids=tuple(matches)),
                receipt,
                failure(
                    operation,
                    FailureKind.CONFLICT,
                    (
                        f"{max(len(matches), 2)} or more Pipedrive {input.object_type} "
                        "carry the identity value, so nothing was written"
                    ),
                ),
            )
        if len(matches) == 1:
            return await self._read_or_update(
                call, input, credentials, matches[0], update_body, earlier_create, fence
            )
        if earlier_create:
            return _uncertain(operation, "Pipedrive search finds no record with the identity value", receipt)

        async def send(deadline: datetime) -> Exchange:
            return await self.client.exchange(
                call,
                operation,
                credentials,
                "POST",
                f"/api/v2/{input.object_type}",
                body=create_body,
                allow_refresh=False,
            )

        try:
            created = await fence.dispatch(send)
        except (CheckpointError, InsufficientAttemptTimeError):
            return MutationAttempt.retry(
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    (
                        "The single-dispatch fence could not authorize a send or retry; "
                        "the next attempt must reconcile the checkpoint"
                    ),
                )
            )
        except Exception:
            return _uncertain(operation, "create dispatch could not be confirmed", Receipt())
        receipt = self.client.receipt(call, created)
        if created.status == 401 and credentials.auth_method_id == "pipedrive-oauth":
            refreshed, rejected_failure = await self.client.refresh_after_write_rejection(
                call, credentials, operation
            )
            if refreshed:
                problem = await _record_can_resend(fence, operation)
                return problem or MutationAttempt.retry(cast(Failure, created.failure))
            created.failure = rejected_failure
        if (
            created.outcome in ("notSent", "rateLimited")
            or created.outcome == "unavailable"
            and created.status == 503
        ):
            problem = await _record_can_resend(fence, operation)
            return problem or MutationAttempt.retry(cast(Failure, created.failure), created.retry_after)
        if created.outcome == "defect":
            return MutationAttempt.branch_result(
                definitions.DEFECT, _upsert_empty(), receipt, created.failure
            )
        if created.outcome in ("notFound", "rejected"):
            return MutationAttempt.branch_result(
                definitions.PROVIDER_REJECTED, _upsert_empty(), receipt, created.failure
            )
        if created.outcome in ("invalid", "unavailable"):
            if 200 <= created.status < 300:
                return await self._read_back_created(call, input, credentials, identity_field, receipt, fence)
            return MutationAttempt.retry(cast(Failure, created.failure), created.retry_after)
        try:
            record = decode_record_body(input.object_type, created.body)
        except ValueError:
            return await self._read_back_created(call, input, credentials, identity_field, receipt, fence)
        try:
            await fence.record_provider_id(record.id)
        except Exception:
            pass  # A successful response establishes the ID even if this extra checkpoint fails.
        return MutationAttempt.branch_result(
            definitions.UPSERTED,
            UpsertedObject(record, created=True),
            self.client.receipt(call, created, record.id),
        )

    async def _read_back_created(
        self,
        call: Call,
        input: UpsertObjectInput,
        credentials: Any,
        identity_field: str,
        receipt: Receipt,
        fence: Fence,
    ) -> MutationAttempt[UpsertedObject]:
        matches, has_more, result = await self._find_matches(call, input, credentials, identity_field)
        if result.outcome in ("notSent", "rateLimited", "unavailable"):
            return MutationAttempt.retry(cast(Failure, result.failure), result.retry_after)
        if result.outcome != "succeeded" or len(matches) != 1 or has_more:
            return _uncertain(
                "upsertObject",
                "Pipedrive accepted the create but identity read-back could not confirm one record",
                receipt,
                FailureKind.PROTOCOL,
            )
        read = await self._read_or_update(call, input, credentials, matches[0], None, True, fence)
        if read.kind == "retry":
            return read
        if read.branch != definitions.UPSERTED or read.value is None:
            return _uncertain(
                "upsertObject",
                read.failure.message if read.failure else "read-back failed",
                receipt,
                read.failure.kind if read.failure else FailureKind.PROTOCOL,
            )
        record = read.value.object
        try:
            await fence.record_provider_id(record.id)
        except Exception:
            pass  # The confirmed read-back still identifies the created record.
        return MutationAttempt.branch_result(
            definitions.UPSERTED,
            UpsertedObject(record, created=True),
            self.client.receipt(call, result, record.id),
        )
