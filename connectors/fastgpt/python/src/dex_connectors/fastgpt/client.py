# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""FastGPT knowledge base detail Query with Go-compatible classification."""

import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_DATASET_DETAILS_DEFINITION,
    GET_DATASET_DETAILS_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetDatasetDetailsInput, GetDatasetDetailsOutput


class Client:
    """Resolve current personal Key per attempt; close owned transport after Worker shutdown."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate settings without calling FastGPT; injected transports remain caller-owned."""
        if config is None or credentials is None:
            raise ValueError("FastGPT configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("FastGPT timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def get_dataset_details(self) -> "GetDatasetDetailsOperation":
        """Bind the read-only dataset detail Query."""
        return GetDatasetDetailsOperation(self)

    async def aclose(self) -> None:
        """Close this client's HTTP transport when owned."""
        if self.owns_http_client:
            await self.http_client.aclose()


class GetDatasetDetailsOperation:
    """Return only ID, name, type, and status for an accessible knowledge base."""

    def __init__(self, client: Client) -> None:
        """Bind the validated client."""
        if client is None:
            raise ValueError("FastGPT client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branches."""
        return GET_DATASET_DETAILS_DEFINITION

    async def invoke(
        self, call: Call, input: GetDatasetDetailsInput
    ) -> QueryAttempt[GetDatasetDetailsOutput]:
        """Dispatch a single request and classify definite or repeatable outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "personal API Key is unavailable or invalid")
        if not _valid_api_key(api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "personal API Key is unavailable or invalid")
        if not isinstance(input, GetDatasetDetailsInput) or not _valid_id(input.id):
            return _branch(DEFECT, FailureKind.VALIDATION, "knowledge base ID is invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/api/core/dataset/detail?" + urlencode({"id": input.id}),
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_key},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "FastGPT request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "FastGPT rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "FastGPT is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "FastGPT response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "FastGPT response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"FastGPT rejected the request with HTTP {status}")
            wire = _decode_wire(body)
            if wire is None:
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "FastGPT returned invalid knowledge base data"
                )
            code = wire["code"]
            if code != 200:
                if code == 429:
                    return _retry(FailureKind.RATE_LIMIT, "FastGPT rate limited the request", retry_after)
                if code >= 500:
                    return _retry(FailureKind.AVAILABILITY, "FastGPT is temporarily unavailable", retry_after)
                kind = (
                    FailureKind.AUTHENTICATION
                    if code == 401
                    else FailureKind.AUTHORIZATION
                    if code == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, "FastGPT rejected the knowledge base request")
            data = wire.get("data")
            if not isinstance(data, dict) or not _valid_metadata(data, input.id):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "FastGPT returned invalid knowledge base metadata"
                )
            return QueryAttempt.branch_result(
                FOUND,
                GetDatasetDetailsOutput(data["_id"], data["name"], data["type"], data["status"]),
                Receipt(),
            )
        finally:
            await response.aclose()


def _valid_api_key(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _valid_id(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 128
        and value.strip() == value
        and not any(c in value for c in "/?#&")
    )


def _decode_wire(body: bytes) -> dict[str, Any] | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    return wire if isinstance(wire, dict) and type(wire.get("code")) is int else None


def _valid_metadata(data: dict[str, Any], requested_id: str) -> bool:
    return data.get("_id") == requested_id and all(
        isinstance(data.get(field), str) and 0 < len(data[field]) <= limit
        for field, limit in (("name", 512), ("type", 128), ("status", 128))
    )


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetDatasetDetailsOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetDatasetDetailsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_DATASET_DETAILS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetDatasetDetailsOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_DATASET_DETAILS_OPERATION_ID, message), after)
