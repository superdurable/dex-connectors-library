# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Algolia index search and record replacement with Go-compatible classification."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
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
    StepDefaults,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    SAVE_OBJECT_DEFINITION as SAVE_OBJECT_DEFINITION,
)
from ._generated.definitions import (
    SAVE_OBJECT_OPERATION_ID as SAVE_OBJECT_OPERATION_ID,
)
from ._generated.definitions import (
    SAVED as SAVED,
)
from ._generated.definitions import (
    SEARCH_DEFINITION as SEARCH_DEFINITION,
)
from ._generated.definitions import (
    SEARCH_OPERATION_ID as SEARCH_OPERATION_ID,
)
from ._generated.definitions import (
    SEARCHED as SEARCHED,
)
from .models import SavedObject, SaveObjectInput, SearchHit, SearchInput, SearchPage

PUBLIC_HOST_TEMPLATE = "https://algolia.net/1"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_RECORD_PAYLOAD_BYTES = 1 << 20
_MAX_HITS_PER_PAGE = 1000
_APPLICATION_ID = re.compile(r"[A-Z0-9]+")
_PATH_SEGMENT_SAFE = "$&+:=@"
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_DEFECT_BRANCH = BranchDefinition(DEFECT, "Local input or credentials are invalid.", True)






@dataclass(frozen=True, slots=True)
class _ProviderReply:
    """A successful HTTP 200 body, or the non-200 status that selects providerRejected."""

    status: int
    body: bytes


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Algolia configuration is required")
        if credentials is None:
            raise ValueError("Algolia credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Algolia timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or PUBLIC_HOST_TEMPLATE)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def search(self) -> "SearchOperation":
        """Bind the index search Query to this client."""
        return SearchOperation(self)

    def save_object(self) -> "SaveObjectOperation":
        """Bind the record replacement Mutation to this client."""
        return SaveObjectOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def send(
        self, call: Call, operation: str, method: str, path: str, body: bytes
    ) -> _ProviderReply | Failure | tuple[Failure, float]:
        """Send one request; a tuple means retry, an AUTHENTICATION or LOCAL_DEFECT
        Failure means defect, and any other Failure means invalidResponse."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        application_id = "" if credentials is None else credentials.application_id
        api_key = "" if credentials is None else credentials.api_key.reveal()
        if (
            not isinstance(application_id, str)
            or _APPLICATION_ID.fullmatch(application_id) is None
            or not api_key
            or not is_header_safe_credential(api_key)
        ):
            return _failure(
                operation, FailureKind.AUTHENTICATION, "Algolia credentials are unavailable or invalid"
            )
        base_url = self.base_url
        if base_url == PUBLIC_HOST_TEMPLATE:
            base_url = f"https://{application_id}.algolia.net/1"
        try:
            request = self.http_client.build_request(
                method,
                base_url + path,
                content=body,
                headers={
                    "x-algolia-api-key": api_key,
                    "x-algolia-application-id": application_id,
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _failure(operation, FailureKind.LOCAL_DEFECT, "Algolia request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _failure(operation, FailureKind.TRANSPORT, "Algolia response is unavailable"), 0.0
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                return (
                    _failure(operation, FailureKind.AVAILABILITY, "Algolia is temporarily unavailable"),
                    parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC)),
                )
            if status != 200:
                return _ProviderReply(status, b"")
            try:
                return _ProviderReply(status, await read_bounded_body(response, self.max_response_bytes))
            except BodyTooLargeError:
                return _failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "Algolia response exceeded the limit"
                )
            except ProviderTransportError:
                return _failure(operation, FailureKind.AVAILABILITY, "Algolia response was interrupted"), 0.0
        finally:
            await response.aclose()


class SearchOperation:
    """Search one Algolia index by query phrase."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Algolia client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SEARCH_DEFINITION

    async def invoke(self, call: Call, input: SearchInput) -> QueryAttempt[SearchPage]:
        """Validate, search once, then select a branch or safe retry."""
        operation = SEARCH_OPERATION_ID
        if (
            not isinstance(input, SearchInput)
            or not _is_valid_path_part(input.index_name)
            or not isinstance(input.query, str)
            or type(input.hits_per_page) is not int
            or not 0 <= input.hits_per_page <= _MAX_HITS_PER_PAGE
        ):
            return _query_branch(DEFECT, FailureKind.VALIDATION, "indexName or hitsPerPage is invalid")
        parameters = {"query": input.query}
        if input.hits_per_page > 0:
            parameters["hitsPerPage"] = str(input.hits_per_page)
        payload = json.dumps({"params": urlencode(sorted(parameters.items()))}).encode()
        path = "/indexes/" + quote(input.index_name, safe=_PATH_SEGMENT_SAFE) + "/query"
        reply = await self.client.send(call, operation, "POST", path, payload)
        if isinstance(reply, tuple):
            return QueryAttempt.retry(reply[0], reply[1])
        if isinstance(reply, Failure):
            branch = DEFECT if _is_local_failure(reply) else INVALID_RESPONSE
            return QueryAttempt.branch_result(branch, SearchPage(), Receipt(), reply)
        if reply.status != 200:
            return _query_branch(
                PROVIDER_REJECTED,
                _rejection_kind(reply.status),
                f"Algolia rejected search with HTTP {reply.status}",
            )
        try:
            page = _decode_search_page(json.loads(reply.body))
        except (ValueError, TypeError, UnicodeDecodeError):
            return _query_branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Algolia returned an invalid search page"
            )
        if any(not hit.object_id for hit in page.hits):
            return _query_branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Algolia returned a hit without objectID"
            )
        return QueryAttempt.branch_result(SEARCHED, page, Receipt())


class SaveObjectOperation:
    """Replace one record by stable objectID; Algolia queues indexing asynchronously."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Algolia client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SAVE_OBJECT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: SaveObjectInput) -> IdempotencyKey:
        """Use Dex's call ID; a stable object ID and absolute replacement make retries safe."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: SaveObjectInput) -> MutationAttempt[SavedObject]:
        """Validate, replace the record once, then select a branch or safe retry."""
        operation = SAVE_OBJECT_OPERATION_ID
        if (
            not isinstance(input, SaveObjectInput)
            or not _is_valid_path_part(input.index_name)
            or not _is_valid_path_part(input.object_id)
            or not isinstance(input.attributes, dict)
        ):
            return _mutation_branch(
                DEFECT, FailureKind.VALIDATION, "indexName, objectID, and attributes are required"
            )
        if "objectID" in input.attributes:
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "objectID belongs in its dedicated field")
        if any(not isinstance(key, str) or not key.strip() for key in input.attributes):
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "attribute names must be nonblank")
        try:
            payload = json.dumps({**input.attributes, "objectID": input.object_id}).encode()
        except (TypeError, ValueError):
            payload = b""
        if not payload or len(payload) > _MAX_RECORD_PAYLOAD_BYTES:
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "record payload is invalid or too large")
        path = (
            "/indexes/"
            + quote(input.index_name, safe=_PATH_SEGMENT_SAFE)
            + "/"
            + quote(input.object_id, safe=_PATH_SEGMENT_SAFE)
        )
        reply = await self.client.send(call, operation, "PUT", path, payload)
        if isinstance(reply, tuple):
            return MutationAttempt.retry(reply[0], reply[1])
        if isinstance(reply, Failure):
            branch = DEFECT if _is_local_failure(reply) else INVALID_RESPONSE
            return MutationAttempt.branch_result(branch, SavedObject(), Receipt(), reply)
        if reply.status != 200:
            return _mutation_branch(
                PROVIDER_REJECTED,
                _rejection_kind(reply.status),
                f"Algolia rejected record replacement with HTTP {reply.status}",
            )
        try:
            saved = _decode_saved_object(json.loads(reply.body))
        except (ValueError, TypeError, UnicodeDecodeError):
            saved = None
        if saved is None or saved.object_id != input.object_id or saved.task_id <= 0:
            return _mutation_branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Algolia returned an invalid indexing task"
            )
        return MutationAttempt.branch_result(SAVED, saved, Receipt())


def _decode_search_page(document: Any) -> SearchPage:
    if not isinstance(document, dict):
        raise ValueError
    nb_hits = document.get("nbHits", 0)
    hits = document.get("hits")
    if type(nb_hits) is not int or nb_hits < 0 or not isinstance(hits, list):
        raise ValueError
    return SearchPage(nb_hits, tuple(_decode_search_hit(hit) for hit in hits))


def _decode_search_hit(document: Any) -> SearchHit:
    if not isinstance(document, dict):
        raise ValueError
    object_id = document.get("objectID") or ""
    name = document.get("name") or ""
    if not isinstance(object_id, str) or not isinstance(name, str):
        raise ValueError
    return SearchHit(object_id, name)


def _decode_saved_object(document: Any) -> SavedObject:
    if not isinstance(document, dict):
        raise ValueError
    object_id = document.get("objectID") or ""
    task_id = document.get("taskID") or 0
    if not isinstance(object_id, str) or type(task_id) is not int:
        raise ValueError
    return SavedObject(object_id, task_id)


def _is_valid_path_part(value: object) -> bool:
    return (
        isinstance(value, str) and bool(value.strip()) and not any(character in value for character in "/?&#")
    )


def _is_local_failure(failure: Failure) -> bool:
    return failure.kind in (FailureKind.AUTHENTICATION, FailureKind.LOCAL_DEFECT)


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _query_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SearchPage]:
    return QueryAttempt.branch_result(
        branch, SearchPage(), Receipt(), _failure(SEARCH_OPERATION_ID, kind, message)
    )


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[SavedObject]:
    return MutationAttempt.branch_result(
        branch, SavedObject(), Receipt(), _failure(SAVE_OBJECT_OPERATION_ID, kind, message)
    )
