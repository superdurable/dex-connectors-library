# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Baichuan knowledge-base query with Go-compatible provider decisions."""

import json
from datetime import UTC, datetime
from typing import Any

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
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

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    DEFECT,
    INVALID_RESPONSE,
    LIST_KNOWLEDGE_BASES_DEFINITION,
    LISTED,
    PROVIDER_REJECTED,
)
from .models import KnowledgeBase, ListKnowledgeBasesInput, ListKnowledgeBasesOutput


class Client:
    """Own a redirect-safe HTTP transport and resolve the API key for every query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Baichuan configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Baichuan timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_knowledge_bases(self) -> "ListKnowledgeBasesOperation":
        """Bind the knowledge-base list Query to this client."""
        return ListKnowledgeBasesOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after the Worker stops."""
        await self.http_client.aclose()


class ListKnowledgeBasesOperation:
    """Read all knowledge bases visible to the configured Baichuan key."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Baichuan client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the stable list operation and branch contract."""
        return LIST_KNOWLEDGE_BASES_DEFINITION

    async def invoke(
        self, call: Call, input: ListKnowledgeBasesInput
    ) -> QueryAttempt[ListKnowledgeBasesOutput]:
        """Fetch one bounded provider response and classify its outcome."""
        if not isinstance(input, ListKnowledgeBasesInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "knowledge-base input is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        if not isinstance(credentials, Credentials) or credentials.api_key is None:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        api_key = credentials.api_key.reveal()
        if not _valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/v1/kbs",
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Baichuan request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Baichuan rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Baichuan is temporarily unavailable", retry_after)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Baichuan rejected the request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Baichuan response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Baichuan response was interrupted")
            try:
                output = _decode_response(body)
            except (UnicodeDecodeError, ValueError, TypeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Baichuan returned an invalid knowledge-base list"
                )
            return QueryAttempt.branch_result(LISTED, output, Receipt())
        finally:
            await response.aclose()


def _valid_api_key(api_key: object) -> bool:
    return (
        isinstance(api_key, str) and 0 < len(api_key) <= 512 and all(32 < ord(char) < 127 for char in api_key)
    )


def _decode_response(body: bytes) -> ListKnowledgeBasesOutput:
    document: Any = json.loads(
        body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid number"))
    )
    if (
        not isinstance(document, dict)
        or document.get("object") != "list"
        or not isinstance(document.get("data"), list)
    ):
        raise ValueError("invalid list envelope")
    items = document["data"]
    if len(items) > 10_000:
        raise ValueError("knowledge-base list exceeds limit")
    seen: set[str] = set()
    bases: list[KnowledgeBase] = []
    for item in items:
        if not isinstance(item, dict):
            raise ValueError("invalid knowledge base")
        identifier = item.get("id")
        name = item.get("name", "")
        description = item.get("description", "")
        status = item.get("status", "")
        file_ids = item.get("file_ids")
        created = item.get("created_at", 0)
        updated = item.get("updated_at", 0)
        if (
            not isinstance(identifier, str)
            or not identifier
            or len(identifier) > 256
            or identifier in seen
            or item.get("object") != "knowledge-base"
            or not isinstance(name, str)
            or len(name) > 256
            or not isinstance(description, str)
            or len(description) > 4096
            or not isinstance(status, str)
            or file_ids is not None
            and not isinstance(file_ids, list)
            or isinstance(file_ids, list)
            and (
                len(file_ids) > 10_000
                or any(not isinstance(value, str) or not value or len(value) > 256 for value in file_ids)
            )
            or type(created) is not int
            or created < 0
            or type(updated) is not int
            or updated < 0
        ):
            raise ValueError("invalid knowledge base")
        seen.add(identifier)
        bases.append(
            KnowledgeBase(identifier, name, description, status, tuple(file_ids or ()), created, updated)
        )
    return ListKnowledgeBasesOutput(tuple(bases))


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListKnowledgeBasesOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListKnowledgeBasesOutput(),
        Receipt(),
        Failure(kind, "baichuan", "listKnowledgeBases", message),
    )


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[ListKnowledgeBasesOutput]:
    return QueryAttempt.retry(Failure(kind, "baichuan", "listKnowledgeBases", message), retry_after)
