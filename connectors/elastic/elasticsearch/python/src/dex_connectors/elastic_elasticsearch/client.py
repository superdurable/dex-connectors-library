# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Elasticsearch fixed-ID writes and bounded search operations."""

import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote, urlencode

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
    Receipt,
)
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as _GeneratedConfig
from ._generated.config import Credentials as _GeneratedCredentials
from ._generated.definitions import (
    ABSENT,
    DEFECT,
    DELETE_DOCUMENT_DEFINITION,
    FOUND,
    INDEX_DOCUMENT_DEFINITION,
    INDEX_NOT_FOUND,
    INDEXED,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    SEARCH_DOCUMENTS_DEFINITION,
)
from .models import (
    DeletedDocument,
    DeleteDocumentInput,
    IndexDocumentInput,
    IndexedDocument,
    SearchDocumentsInput,
    SearchHit,
    SearchPage,
)

_INDEX = re.compile(r"^[a-z0-9.][a-z0-9._-]*$")


class Config(_GeneratedConfig):
    """Cluster URL and bounded request and response settings."""

    __slots__ = ()


class Credentials(_GeneratedCredentials):
    """Internal user and password resolved at call time."""

    __slots__ = ()


@dataclass(frozen=True, slots=True)
class Exchange:
    """Bounded provider response or sanitized transport outcome."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


class Client:
    """Use one redirect-safe HTTP client and call-time Basic credentials."""

    def __init__(
        self, config: Config, credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Elasticsearch configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0 or config.max_document_bytes <= 0:
            raise ValueError("Elasticsearch limits must be positive")
        self.cluster_url = validate_base_url(config.cluster_url)
        self.max_response_bytes = config.max_response_bytes
        self.max_document_bytes = config.max_document_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def index_document(self) -> "IndexDocumentOperation":
        """Bind the absolute document replacement Mutation."""
        return IndexDocumentOperation(self)

    def delete_document(self) -> "DeleteDocumentOperation":
        """Bind the stable-ID deletion Mutation."""
        return DeleteDocumentOperation(self)

    def search_documents(self) -> "SearchDocumentsOperation":
        """Bind the bounded document search Query."""
        return SearchDocumentsOperation(self)

    async def aclose(self) -> None:
        """Close the provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()

    async def authorization(self, call: Call) -> str:
        """Build Basic authorization without returning credentials in failures."""
        try:
            credentials = await self.credentials.resolve(call)
            username, password = credentials.username, credentials.password.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return ""
        if not username or any(character in username for character in ":\r\n\x00"):
            return ""
        if not password or any(character in password for character in "\r\n\x00"):
            return ""
        return "Basic " + base64.b64encode(f"{username}:{password}".encode()).decode("ascii")

    async def request(self, request: httpx.Request) -> Exchange:
        """Read one bounded response and classify transport interruption."""
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
                error = ""
            except BodyTooLargeError:
                body, error = b"", "too_large"
            except ProviderTransportError:
                body, error = b"", "read"
            return Exchange(response.status_code, body, retry_after, error)
        finally:
            await response.aclose()


class IndexDocumentOperation:
    """Fully replace one document under a stable caller-supplied ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Elasticsearch client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated indexing contract."""
        return INDEX_DOCUMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: IndexDocumentInput) -> IdempotencyKey:
        """Use stable Dex call identity; absolute PUT makes repetition safe."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: IndexDocumentInput) -> MutationAttempt[IndexedDocument]:
        """Replace the source and retry unconfirmed responses with the same ID."""
        if (
            not _valid_index(input.index)
            or not _valid_document_id(input.document_id)
            or not isinstance(input.source, dict)
        ):
            return _index_failure(DEFECT, FailureKind.VALIDATION)
        source = json.dumps(input.source, ensure_ascii=False, separators=(",", ":")).encode()
        if len(source) > self.client.max_document_bytes:
            return _index_failure(DEFECT, FailureKind.VALIDATION)
        authorization = await self.client.authorization(call)
        if not authorization:
            return _index_failure(DEFECT, FailureKind.AUTHENTICATION)
        request = self.client.http_client.build_request(
            "PUT", _document_url(self.client.cluster_url, input.index, input.document_id),
            headers={"Accept": "application/json", "Content-Type": "application/json",
                     "Authorization": authorization}, content=source,
        )
        exchange = await self.client.request(request)
        retry = _document_retry(exchange, "indexDocument")
        if retry is not None:
            return retry
        if exchange.status in (200, 201):
            body = _parse_json(exchange.body)
            if not isinstance(body, dict) or body.get("_index") != input.index or \
                    body.get("_id") != input.document_id or body.get("result") not in ("created", "updated"):
                return _index_retry(FailureKind.PROTOCOL)
            return MutationAttempt.branch_result(
                INDEXED, IndexedDocument(input.index, input.document_id), Receipt()
            )
        return _index_failure(PROVIDER_REJECTED, _rejection_kind(exchange.status))


class DeleteDocumentOperation:
    """Ensure one stable document ID is absent."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Elasticsearch client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated deletion contract."""
        return DELETE_DOCUMENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: DeleteDocumentInput) -> IdempotencyKey:
        """Use stable Dex call identity; repeated DELETE is safe."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: DeleteDocumentInput) -> MutationAttempt[DeletedDocument]:
        """Classify a deleted or already absent document as one successful outcome."""
        if not _valid_index(input.index) or not _valid_document_id(input.document_id):
            return _delete_failure(DEFECT, FailureKind.VALIDATION)
        authorization = await self.client.authorization(call)
        if not authorization:
            return _delete_failure(DEFECT, FailureKind.AUTHENTICATION)
        request = self.client.http_client.build_request(
            "DELETE", _document_url(self.client.cluster_url, input.index, input.document_id),
            headers={"Accept": "application/json", "Authorization": authorization},
        )
        exchange = await self.client.request(request)
        retry = _document_retry(exchange, "deleteDocument")
        if retry is not None:
            return retry
        if exchange.status in (200, 404):
            body = _parse_json(exchange.body)
            if not isinstance(body, dict):
                return _delete_retry(FailureKind.PROTOCOL)
            error = body.get("error")
            if (
                exchange.status == 404
                and isinstance(error, dict)
                and error.get("type") == "index_not_found_exception"
            ):
                return _delete_failure(INDEX_NOT_FOUND, FailureKind.NOT_FOUND)
            if body.get("_index") != input.index or body.get("_id") != input.document_id or \
                    body.get("result") not in ("deleted", "not_found"):
                return _delete_retry(FailureKind.PROTOCOL)
            return MutationAttempt.branch_result(
                ABSENT,
                DeletedDocument(input.index, input.document_id, body["result"] == "deleted"),
                Receipt(),
            )
        return _delete_failure(PROVIDER_REJECTED, _rejection_kind(exchange.status))


class SearchDocumentsOperation:
    """Search one index with bounded Lucene query-string pagination."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Elasticsearch client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated search contract."""
        return SEARCH_DOCUMENTS_DEFINITION

    async def invoke(self, call: Call, input: SearchDocumentsInput) -> QueryAttempt[SearchPage]:
        """Reject partial results and retry timed-out or shard-failed searches."""
        size = input.size or 10
        if not _valid_index(input.index) or not input.query.strip() or len(input.query.encode()) > 4096 or \
                size < 1 or size > 100 or input.from_ < 0 or input.from_ + size > 10000:
            return _search_failure(DEFECT, FailureKind.VALIDATION)
        authorization = await self.client.authorization(call)
        if not authorization:
            return _search_failure(DEFECT, FailureKind.AUTHENTICATION)
        query = urlencode({"q": input.query, "size": size, "from": input.from_,
                           "allow_partial_search_results": "false"})
        request = self.client.http_client.build_request(
            "GET", f"{self.client.cluster_url}/{input.index}/_search?{query}",
            headers={"Accept": "application/json", "Authorization": authorization},
        )
        exchange = await self.client.request(request)
        if exchange.error == "request":
            return _search_retry(FailureKind.AVAILABILITY)
        if exchange.status == 429:
            return _search_retry(FailureKind.RATE_LIMIT, exchange.retry_after)
        if exchange.status in (408, 409) or exchange.status >= 500:
            return _search_retry(FailureKind.AVAILABILITY, exchange.retry_after)
        if exchange.status == 404:
            return _search_failure(INDEX_NOT_FOUND, FailureKind.NOT_FOUND)
        if exchange.status != 200:
            return _search_failure(PROVIDER_REJECTED, _rejection_kind(exchange.status))
        if exchange.error == "too_large":
            return _search_failure(INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE)
        if exchange.error:
            return _search_retry(FailureKind.AVAILABILITY)
        body = _parse_json(exchange.body)
        if not isinstance(body, dict) or not isinstance(body.get("timed_out"), bool) or \
                not isinstance(body.get("_shards"), dict):
            return _search_failure(INVALID_RESPONSE, FailureKind.PROTOCOL)
        if body["timed_out"] or body["_shards"].get("failed", 0) > 0:
            return _search_retry(FailureKind.AVAILABILITY)
        try:
            hits_root = body["hits"]
            total = hits_root["total"]
            hits = hits_root["hits"]
            if type(total["value"]) is not int or total["value"] < 0 or \
                    total["relation"] not in ("eq", "gte") or not isinstance(hits, list):
                raise ValueError("invalid hits")
            parsed_hits = tuple(_decode_hit(hit) for hit in hits)
            return QueryAttempt.branch_result(
                FOUND, SearchPage(total["value"], total["relation"], parsed_hits), Receipt()
            )
        except (KeyError, TypeError, ValueError):
            return _search_failure(INVALID_RESPONSE, FailureKind.PROTOCOL)


def _decode_hit(hit: Any) -> SearchHit:
    if not isinstance(hit, dict) or not hit.get("_index") or not hit.get("_id") or "_source" not in hit:
        raise ValueError("invalid hit")
    return SearchHit(hit["_index"], hit["_id"], hit.get("_score"), hit["_source"])


def _valid_index(index: str) -> bool:
    return bool(index and len(index) <= 255 and _INDEX.fullmatch(index) and index not in (".", ".."))


def _valid_document_id(document_id: str) -> bool:
    return bool(document_id and len(document_id.encode()) <= 512 and not any(
        character in document_id for character in "\r\n\x00"
    ))


def _document_url(cluster_url: str, index: str, document_id: str) -> str:
    return cluster_url + "/" + index + "/_doc/" + quote(document_id, safe="")


def _parse_json(body: bytes) -> Any:
    try:
        return json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return None


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _document_retry(exchange: Exchange, operation: str) -> MutationAttempt[Any] | None:
    if exchange.status == 429:
        return MutationAttempt.retry(_failure(operation, FailureKind.RATE_LIMIT), exchange.retry_after)
    if exchange.status in (408, 409) or exchange.status >= 500:
        return MutationAttempt.retry(_failure(operation, FailureKind.AVAILABILITY), exchange.retry_after)
    if exchange.error == "too_large":
        return MutationAttempt.retry(_failure(operation, FailureKind.RESPONSE_TOO_LARGE))
    if exchange.error:
        return MutationAttempt.retry(_failure(operation, FailureKind.TRANSPORT))
    return None


def _failure(operation: str, kind: FailureKind) -> Failure:
    return Failure(kind, "elasticsearch", operation, "Elasticsearch provider operation failed")


def _index_failure(branch: str, kind: FailureKind) -> MutationAttempt[IndexedDocument]:
    return MutationAttempt.branch_result(
        branch, IndexedDocument(), Receipt(), _failure("indexDocument", kind)
    )


def _index_retry(kind: FailureKind) -> MutationAttempt[IndexedDocument]:
    return MutationAttempt.retry(_failure("indexDocument", kind))


def _delete_failure(branch: str, kind: FailureKind) -> MutationAttempt[DeletedDocument]:
    return MutationAttempt.branch_result(
        branch, DeletedDocument(), Receipt(), _failure("deleteDocument", kind)
    )


def _delete_retry(kind: FailureKind) -> MutationAttempt[DeletedDocument]:
    return MutationAttempt.retry(_failure("deleteDocument", kind))


def _search_failure(branch: str, kind: FailureKind) -> QueryAttempt[SearchPage]:
    return QueryAttempt.branch_result(branch, SearchPage(), Receipt(), _failure("searchDocuments", kind))


def _search_retry(kind: FailureKind, after: float = 0) -> QueryAttempt[SearchPage]:
    return QueryAttempt.retry(_failure("searchDocuments", kind), after)
