# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Apifox OpenAPI export summary with Go-compatible Query outcomes."""

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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import FOUND as FOUND
from ._generated.definitions import GET_PROJECT_API_SUMMARY_DEFINITION as GET_PROJECT_API_SUMMARY_DEFINITION
from ._generated.definitions import (
    GET_PROJECT_API_SUMMARY_OPERATION_ID as GET_PROJECT_API_SUMMARY_OPERATION_ID,
)
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import GetProjectAPISummaryInput, GetProjectAPISummaryOutput

_ENDPOINT_ROOT = "https://api.apifox.com/v1/projects/"
_EXPORT_BODY = b'{"scope":{"type":"ALL"},"oasVersion":"3.1","exportFormat":"JSON"}'
_HTTP_METHODS = frozenset({"get", "post", "put", "patch", "delete", "head", "options", "trace"})
_OPERATION = GET_PROJECT_API_SUMMARY_OPERATION_ID


class Client:
    """Use one redirect-safe transport and resolve the token for each attempt."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Apifox configuration and credential source are required")
        if not 0 < config.timeout_seconds or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Apifox timeout or response limit is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def get_project_api_summary(self) -> "GetProjectAPISummaryOperation":
        """Bind the project summary Query to this client."""
        return GetProjectAPISummaryOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport after Worker shutdown."""
        await self.http_client.aclose()


class GetProjectAPISummaryOperation:
    """Export one OpenAPI document but return only a bounded summary."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Apifox client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and branches."""
        return GET_PROJECT_API_SUMMARY_DEFINITION

    async def invoke(
        self, call: Call, input: GetProjectAPISummaryInput
    ) -> QueryAttempt[GetProjectAPISummaryOutput]:
        """Summarize an accessible project while keeping its full export out of Flow state."""
        if not isinstance(input, GetProjectAPISummaryInput) or not _valid_project_id(input.project_id):
            return _branch(DEFECT, FailureKind.VALIDATION, "Apifox project ID must be positive decimal")
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Apifox access token is unavailable")
        token = credentials.access_token.reveal()
        if not _valid_token(token):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Apifox access token is invalid")
        endpoint = _ENDPOINT_ROOT + input.project_id + "/export-openapi"
        try:
            request = self.client.http_client.build_request(
                "POST",
                endpoint,
                headers={
                    "Authorization": "Bearer " + token,
                    "X-Apifox-Api-Version": "2024-03-28",
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                content=_EXPORT_BODY,
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "Apifox project request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Apifox rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "Apifox export is temporarily unavailable", retry_after
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"Apifox rejected HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "Apifox export is too large")
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Apifox export was interrupted")
            try:
                summary = _decode_summary(body, token)
            except (UnicodeDecodeError, ValueError, TypeError, KeyError):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Apifox returned invalid OpenAPI data")
            return QueryAttempt.branch_result(FOUND, summary, Receipt())
        finally:
            await response.aclose()


def _valid_project_id(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 20
        and value[0] != "0"
        and value.isascii()
        and value.isdecimal()
    )


def _valid_token(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 1024
        and all(0x20 < ord(character) < 0x7F for character in value)
    )


def _decode_summary(body: bytes, token: str) -> GetProjectAPISummaryOutput:
    document: Any = json.loads(body.decode("utf-8"))
    if not isinstance(document, dict):
        raise ValueError("invalid document")
    version = document.get("openapi")
    info = document.get("info")
    paths = document.get("paths")
    if (
        not isinstance(version, str)
        or not version.startswith("3.1")
        or not isinstance(info, dict)
        or not isinstance(paths, dict)
    ):
        raise ValueError("missing OpenAPI fields")
    title = info.get("title")
    info_version = info.get("version")
    if (
        not isinstance(title, str)
        or not title
        or len(title) > 256
        or not isinstance(info_version, str)
        or not info_version
        or len(info_version) > 64
        or any(character in title + info_version for character in "\r\n\0")
        or token in title + info_version
    ):
        raise ValueError("invalid info")
    count = 0
    for methods in paths.values():
        if not isinstance(methods, dict):
            raise ValueError("invalid paths")
        count += sum(1 for method in methods if method.lower() in _HTTP_METHODS)
    return GetProjectAPISummaryOutput(title, info_version, count)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[GetProjectAPISummaryOutput]:
    return QueryAttempt.branch_result(
        branch, GetProjectAPISummaryOutput(), Receipt(), Failure(kind, CONNECTOR_ID, _OPERATION, message)
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[GetProjectAPISummaryOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, _OPERATION, message), after)
