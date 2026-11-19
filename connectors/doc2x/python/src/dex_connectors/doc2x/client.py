# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Doc2X PDF parse status with Go-compatible outcome classification."""

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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_PARSE_STATUS_DEFINITION,
    GET_PARSE_STATUS_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import GetParseStatusInput, GetParseStatusOutput

_ENDPOINT = "https://v2.doc2x.noedgeai.com/api/v2/parse/status"


class Client:
    """Resolve the API Key per status Query and bound each provider response."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate settings and own the transport unless one was injected."""
        if config is None or credentials is None:
            raise ValueError("Doc2X configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Doc2X configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def get_parse_status(self) -> "GetParseStatusOperation":
        """Bind the PDF task status Query."""
        return GetParseStatusOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class GetParseStatusOperation:
    """Read task state and progress without exposing PDF text or result URLs."""

    def __init__(self, client: Client) -> None:
        """Bind a validated Doc2X client."""
        if client is None:
            raise ValueError("Doc2X client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return GET_PARSE_STATUS_DEFINITION

    async def invoke(self, call: Call, input: GetParseStatusInput) -> QueryAttempt[GetParseStatusOutput]:
        """Dispatch one status read and classify definite and retryable outcomes."""
        if not isinstance(input, GetParseStatusInput) or not _valid_uid(input.uid):
            return _branch(DEFECT, FailureKind.VALIDATION, "Doc2X task identifier is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Doc2X API Key is unavailable or invalid")
        if not _valid_api_key(api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Doc2X API Key is unavailable or invalid")
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT + "?" + urlencode({"uid": input.uid}),
                headers={"Authorization": "Bearer " + api_key, "Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Doc2X status request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Doc2X rate limited status query", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Doc2X status API is temporarily unavailable", retry_after
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch(PROVIDER_REJECTED, kind, f"Doc2X rejected status query with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Doc2X status response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Doc2X status response was interrupted")
            wire = _decode_wire(body)
            if wire is None:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Doc2X returned invalid status data")
            if wire.get("code", "") != "success":
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "Doc2X rejected status query"
                )
            data = wire.get("data")
            if not isinstance(data, dict):
                data = {}
            task_status = data.get("status", "")
            if task_status not in ("processing", "failed", "success"):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Doc2X returned unknown task state")
            progress = data.get("progress")
            if progress is None:
                progress = 0
            if type(progress) is not int or progress < 0 or progress > 100:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Doc2X returned invalid progress")
            return QueryAttempt.branch_result(FOUND, GetParseStatusOutput(task_status, progress), Receipt())
        finally:
            await response.aclose()


def _valid_uid(value: object) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value.encode("utf-8")) <= 256
        and value.strip() == value
        and not any(character in value for character in ("\r", "\n", "\x00"))
    )


def _valid_api_key(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 1024 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _decode_wire(body: bytes) -> dict[str, Any] | None:
    try:
        wire: Any = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict) or not isinstance(wire.get("code", ""), str):
        return None
    data = wire.get("data")
    if data is not None and not isinstance(data, dict):
        return None
    if isinstance(data, dict):
        if "status" in data and not isinstance(data["status"], str):
            return None
        if "progress" in data and data["progress"] is not None and type(data["progress"]) is not int:
            return None
    return wire


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetParseStatusOutput]:
    return QueryAttempt.branch_result(
        branch,
        GetParseStatusOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_PARSE_STATUS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetParseStatusOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, GET_PARSE_STATUS_OPERATION_ID, message), after)
