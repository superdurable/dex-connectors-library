# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Caoliao QR-code page Query with Go-compatible provider decisions."""

import json
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_QR_CODES_DEFINITION
from .models import ListQRCodesInput, ListQRCodesOutput, QRCodeSummary

_LIST_PATH = "/api/v2/rpc/qrcodes/list"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListQRCodesOutput]:
    return QueryAttempt.branch_result(
        branch, ListQRCodesOutput(), Receipt(), Failure(kind, "caoliao", "listQRCodes", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListQRCodesOutput]:
    return QueryAttempt.retry(Failure(kind, "caoliao", "listQRCodes", message), delay)


def _safe_page_token(value: str) -> bool:
    return all(0x21 <= ord(character) <= 0x7E for character in value)


class Client:
    """Own Caoliao settings and resolve the OpenAPI Key per request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Caoliao settings and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Caoliao timeout and response limit must be positive")
        base_url = validate_base_url(config.base_url)
        parsed = urlsplit(base_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc != "open.cli.im"
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Caoliao base URL must be the documented HTTPS production origin")
        self.base_url = base_url.rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_qr_codes(self) -> "ListQRCodesOperation":
        """Bind the QR-code page Query."""
        return ListQRCodesOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class ListQRCodesOperation:
    """Read one bounded page of QR-code names and coding identifiers."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Caoliao client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated QR-code list Query identity and retry policy."""
        return LIST_QR_CODES_DEFINITION

    async def invoke(self, call: Call, input: ListQRCodesInput) -> QueryAttempt[ListQRCodesOutput]:
        """Make one provider request and classify its observed outcome."""
        page_size = input.page_size if isinstance(input, ListQRCodesInput) else -1
        page_token = input.page_token if isinstance(input, ListQRCodesInput) else ""
        if (
            type(page_size) is not int
            or page_size < 0
            or page_size > 100
            or not isinstance(page_token, str)
            or len(page_token) > 4096
            or not _safe_page_token(page_token)
        ):
            return _branch("defect", FailureKind.VALIDATION, "Caoliao page size or token is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
            if not is_header_safe_credential(api_key):
                raise ValueError("invalid Caoliao API Key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "Caoliao API key is unavailable or invalid")
        payload: dict[str, object] = {}
        if page_size:
            payload["page_size"] = page_size
        if page_token:
            payload["page_token"] = page_token
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + _LIST_PATH,
                headers={
                    "Authorization": f"Bearer {api_key}",
                    "Content-Type": "application/json",
                    "Accept": "application/json",
                },
                content=json.dumps(payload, separators=(",", ":")).encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Caoliao page request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Caoliao QR-code list transport failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Caoliao QR-code list rate limited", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Caoliao QR-code list temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Caoliao QR-code list response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Caoliao QR-code list response was interrupted")
            if status != 200:
                kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
                return _branch("providerRejected", kind, f"Caoliao rejected QR-code list with HTTP {status}")
        finally:
            await response.aclose()
        return _decode_response(body, delay)


def _decode_response(body: bytes, delay: float) -> QueryAttempt[ListQRCodesOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Caoliao returned invalid QR-code list JSON")
    if (
        not isinstance(document, dict)
        or type(document.get("code")) is not int
        or type(document.get("error_code", 0)) is not int
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Caoliao returned invalid QR-code list JSON")
    code = document["code"]
    error_code = document.get("error_code", 0)
    if code != 0:
        if code == 429:
            return _retry(FailureKind.RATE_LIMIT, "Caoliao QR-code list rate limited", delay)
        if code == 502 or error_code == 100300:
            return _retry(FailureKind.AVAILABILITY, "Caoliao QR-code list temporarily unavailable", delay)
        kind = (
            FailureKind.AUTHENTICATION
            if code == 401 or error_code in (100315, 100316)
            else FailureKind.PROVIDER_REJECTION
        )
        return _branch("providerRejected", kind, f"Caoliao rejected QR-code list with code {code}")
    data = document.get("data")
    if not isinstance(data, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Caoliao QR-code list is missing")
    pages = data.get("list")
    total = data.get("total", 0)
    next_token = data.get("next_page_token") or ""
    if not isinstance(pages, list) or type(total) is not int or total < 0:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Caoliao QR-code list is missing")
    if (
        len(pages) > 100
        or not isinstance(next_token, str)
        or len(next_token) > 4096
        or not _safe_page_token(next_token)
    ):
        return _branch(
            "invalidResponse", FailureKind.PROTOCOL, "Caoliao returned invalid pagination metadata"
        )
    codes = []
    for page in pages:
        if not isinstance(page, dict):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Caoliao returned invalid QR-code metadata"
            )
        name, coding = page.get("name", ""), page.get("coding", "")
        if (
            not isinstance(name, str)
            or not name
            or len(name.encode()) > 1024
            or not isinstance(coding, str)
            or not coding
            or len(coding) > 256
            or not _safe_page_token(coding)
        ):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Caoliao returned invalid QR-code metadata"
            )
        codes.append(QRCodeSummary(name, coding))
    return QueryAttempt.branch_result("listed", ListQRCodesOutput(tuple(codes), next_token, total), Receipt())
