# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""DataEyes public page reading with Go-compatible provider classification."""

import ipaddress
import json
import re
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
    READ_WEB_PAGE_DEFINITION,
    READ_WEB_PAGE_OPERATION_ID,
)
from .models import ReadWebPageInput, ReadWebPageOutput

_ENDPOINT = "https://api.shuyanai.com/v1/reader"
_DEFINITE_PAGE_CODES = {32000, 32001, 32003, 32004}
_REJECTED_CODES = _DEFINITE_PAGE_CODES | {40001, 40003}
_RETRY_CODES = {32002, 50001}


class Client:
    """Resolve the reading Key for every Query attempt and bound each provider response."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        """Validate settings and own the transport unless one was injected."""
        if config is None or credentials is None:
            raise ValueError("DataEyes configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("DataEyes configuration is invalid")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.owns_http_client = http_client is None

    def read_web_page(self) -> "ReadWebPageOperation":
        """Bind the public page reading Query."""
        return ReadWebPageOperation(self)

    async def aclose(self) -> None:
        """Close the connector-owned transport after the Worker stops."""
        if self.owns_http_client:
            await self.http_client.aclose()


class ReadWebPageOperation:
    """Read one public HTTPS page without returning provider metadata or the URL."""

    def __init__(self, client: Client) -> None:
        """Bind a validated DataEyes client."""
        if client is None:
            raise ValueError("DataEyes client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest operation identity and branches."""
        return READ_WEB_PAGE_DEFINITION

    async def invoke(self, call: Call, input: ReadWebPageInput) -> QueryAttempt[ReadWebPageOutput]:
        """Dispatch one read and classify definite and retryable outcomes."""
        if not isinstance(input, ReadWebPageInput) or not _is_public_page_url(input.url):
            return _branch(
                DEFECT,
                FailureKind.VALIDATION,
                "DataEyes URL must be a public HTTPS page without query or fragment",
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "DataEyes reading Key is unavailable or invalid"
            )
        if not _is_valid_api_key(api_key):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "DataEyes reading Key is unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "POST",
                _ENDPOINT,
                headers={
                    "Authorization": "Bearer " + api_key,
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                json={"url": input.url, "format": "markdown"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "DataEyes reader request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "DataEyes rate limited the read", retry_after)
            if status == 408 or status > 500:
                return _retry(
                    FailureKind.AVAILABILITY, "DataEyes reader is temporarily unavailable", retry_after
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "DataEyes response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "DataEyes response was interrupted")
            wire = _decode_wire(body)
            code = wire.get("code") if wire is not None else None
            if status == 500:
                if code in _DEFINITE_PAGE_CODES:
                    return _branch(
                        PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "DataEyes could not read this page"
                    )
                return _retry(
                    FailureKind.AVAILABILITY, "DataEyes reader is temporarily unavailable", retry_after
                )
            if type(code) is int and code != 0:
                if code in _REJECTED_CODES:
                    return _branch(
                        PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, "DataEyes could not read this page"
                    )
                if code in _RETRY_CODES:
                    return _retry(
                        FailureKind.AVAILABILITY, "DataEyes reader is temporarily unavailable", retry_after
                    )
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"DataEyes rejected reading with code {code}",
                )
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, f"DataEyes rejected reading with HTTP {status}")
            output = _decode_page(wire)
            if output is None:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "DataEyes returned invalid page data")
            return QueryAttempt.branch_result(FOUND, output, Receipt())
        finally:
            await response.aclose()


def _is_public_page_url(value: object) -> bool:
    if not isinstance(value, str) or not value or len(value.encode("utf-8")) > 2048 or value.strip() != value:
        return False
    if not value.startswith("https://") or "?" in value or "#" in value:
        return False
    try:
        parsed = urlsplit(value)
        hostname = parsed.hostname
        port = parsed.port
    except ValueError:
        return False
    if parsed.scheme != "https" or not hostname or parsed.username is not None or parsed.password is not None:
        return False
    if port not in (None, 443) or "." not in hostname or not re.fullmatch(r"[A-Za-z0-9.-]+", hostname):
        return False
    try:
        ipaddress.ip_address(hostname)
    except ValueError:
        return True
    return False


def _is_valid_api_key(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 1024
        and all(0x21 <= ord(character) <= 0x7E for character in value)
    )


def _decode_wire(body: bytes) -> dict[str, Any] | None:
    try:
        wire = json.loads(body.decode("utf-8"))
    except (UnicodeDecodeError, ValueError):
        return None
    if not isinstance(wire, dict):
        return None
    code = wire.get("code")
    if code is not None and type(code) is not int:
        return None
    return wire


def _decode_page(wire: dict[str, Any] | None) -> ReadWebPageOutput | None:
    if wire is None or wire.get("code") != 0:
        return None
    data = wire.get("data")
    if not isinstance(data, dict):
        return None
    markdown = data.get("markdown")
    metadata = data.get("metadata")
    if not isinstance(markdown, str) or metadata is not None and not isinstance(metadata, dict):
        return None
    title = metadata.get("title", "") if isinstance(metadata, dict) else ""
    if (
        not isinstance(title, str)
        or len(title.encode("utf-8")) > 1000
        or len(markdown.encode("utf-8")) > 100000
    ):
        return None
    return ReadWebPageOutput(title, markdown)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ReadWebPageOutput]:
    return QueryAttempt.branch_result(
        branch,
        ReadWebPageOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, READ_WEB_PAGE_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[ReadWebPageOutput]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, READ_WEB_PAGE_OPERATION_ID, message), after)
