# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Notion HTTP transport and Go-compatible response classification."""

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any, cast
from urllib.parse import quote

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.projectconfig.broker import CredentialBrokerUnavailableError
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID

if TYPE_CHECKING:
    from .operations import (
        CreatePageOperation,
        GetPageOperation,
        QueryDatabaseOperation,
        SearchOperation,
        UpdatePagePropertiesOperation,
    )

NOTION_VERSION = "2026-03-11"


@dataclass(frozen=True, slots=True)
class ProviderResponse:
    """A bounded provider result with dispatch evidence and no secret body."""

    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    error: ProviderTransportError | None = None
    too_large: bool = False
    interrupted: bool = False
    reflects_secret: bool = False
    not_built: bool = False


@dataclass(frozen=True, slots=True)
class Classification:
    """The provider outcome before an operation chooses its branch."""

    outcome: str
    failure: Failure | None = None
    retry_after: float = 0
    committed_id: str = ""


class Client:
    """Own one Notion transport and resolve private credentials per invocation."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Notion configuration and credentials are required")
        endpoint = validate_base_url(config.endpoint or Config().endpoint)
        self.endpoint = endpoint.rstrip("/")
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if self.max_response_bytes <= 0:
            raise ValueError("Notion maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(60)
        self.owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the transport when this client owns it."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def search(self) -> "SearchOperation":
        """Return the Notion title search Query."""
        from .operations import SearchOperation

        return SearchOperation(self)

    def query_database(self) -> "QueryDatabaseOperation":
        """Return the Notion data source Query."""
        from .operations import QueryDatabaseOperation

        return QueryDatabaseOperation(self)

    def get_page(self) -> "GetPageOperation":
        """Return the Notion page read Query."""
        from .operations import GetPageOperation

        return GetPageOperation(self)

    def create_page(self) -> "CreatePageOperation":
        """Return the fenced Notion page creation Mutation."""
        from .operations import CreatePageOperation

        return CreatePageOperation(self)

    def update_page_properties(self) -> "UpdatePagePropertiesOperation":
        """Return the absolute Notion property update Mutation."""
        from .operations import UpdatePagePropertiesOperation

        return UpdatePagePropertiesOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[str | None, Failure | None, bool]:
        """Classify unavailable broker credentials separately from invalid tokens."""
        try:
            credentials = await self.credentials.resolve(call)
        except (CredentialBrokerUnavailableError, TimeoutError):
            return (
                None,
                failure(
                    operation, FailureKind.AUTHENTICATION, "Notion connection credentials are unavailable"
                ),
                True,
            )
        except Exception:
            return (
                None,
                failure(
                    operation, FailureKind.AUTHENTICATION, "Notion connection credentials are unavailable"
                ),
                False,
            )
        if not isinstance(credentials, Credentials) or not is_header_safe_credential(
            credentials.api_token.reveal()
        ):
            return (
                None,
                failure(operation, FailureKind.AUTHENTICATION, "Notion connection API token is invalid"),
                False,
            )
        return credentials.api_token.reveal(), None, False

    async def request(
        self,
        token: str,
        method: str,
        path: str,
        *,
        payload: object | None = None,
        query: list[tuple[str, str]] | None = None,
        timeout: float = 20,
    ) -> ProviderResponse:
        """Send one request without redirects and read at most the configured body limit."""
        headers = {
            "Authorization": f"Bearer {token}",
            "Notion-Version": NOTION_VERSION,
            "Accept": "application/json",
        }
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method,
                self.endpoint + "/v1" + path,
                params=httpx.QueryParams(cast(Any, query)) if query else None,
                content=json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
                if payload is not None
                else None,
                headers=headers,
                timeout=timeout,
            )
        except (ValueError, TypeError, httpx.HTTPError):
            return ProviderResponse(not_built=True)
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError as error:
            return ProviderResponse(error=error)
        limit = self.max_response_bytes if 200 <= response.status_code < 300 else 65536
        try:
            body = await read_bounded_body(response, limit)
        except BodyTooLargeError:
            return ProviderResponse(response.status_code, response.headers, too_large=True)
        except ProviderTransportError:
            return ProviderResponse(response.status_code, response.headers, interrupted=True)
        if token.encode() in body:
            return ProviderResponse(response.status_code, response.headers, reflects_secret=True)
        return ProviderResponse(response.status_code, response.headers, body)

    def receipt(self, call: Call, response: ProviderResponse, object_id: str = "") -> Receipt:
        """Record only safe Notion correlation fields."""
        request_id = (response.headers or httpx.Headers()).get("X-Notion-Request-Id", "")
        if not _safe_token(request_id):
            request_id = _token(response.body, "/request_id")
        return Receipt(
            call_id=call.id,
            idempotency_key=call.idempotency_key,
            provider=CONNECTOR_ID,
            provider_object_id=object_id,
            provider_request_id=request_id,
            observed_at=datetime.now(UTC),
        )


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe Notion failure."""
    return Failure(kind, CONNECTOR_ID, operation, message)


def _token(body: bytes, pointer: str) -> str:
    values = read_error_tokens(body, (pointer,))
    return values[0] if values else ""


def _safe_token(value: str) -> bool:
    return bool(value) and len(value) <= 128 and all(" " < char <= "~" for char in value)


def _retry_after(response: ProviderResponse) -> float:
    return parse_retry_after((response.headers or httpx.Headers()).get("Retry-After", ""), datetime.now(UTC))


def _rejection(operation: str, response: ProviderResponse) -> Failure:
    kind = {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        404: FailureKind.NOT_FOUND,
        409: FailureKind.CONFLICT,
        429: FailureKind.RATE_LIMIT,
    }.get(response.status, FailureKind.PROVIDER_REJECTION)
    code = _token(response.body, "/code")
    return failure(
        operation, kind, f"Notion rejected HTTP {response.status}" + (f" ({code})" if code else "")
    )


def classify_read(operation: str, response: ProviderResponse) -> Classification:
    """Classify one read, including local network refusal as Retry."""
    if response.not_built:
        return Classification(
            "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Notion request could not be built")
        )
    if response.error:
        return Classification(
            "retry", failure(operation, FailureKind.TRANSPORT, "Notion read is temporarily unavailable")
        )
    status = response.status
    if (
        status == 429
        and _token(response.body, "/additional_data/rate_limit_reason") == "public_api_request_blocked"
    ):
        return Classification(
            "rejected", failure(operation, FailureKind.PROVIDER_REJECTION, "Notion blocked this connection")
        )
    if status in (429, 529):
        return Classification(
            "retry",
            failure(operation, FailureKind.RATE_LIMIT, "Notion rate limited the read"),
            _retry_after(response),
        )
    if status == 404:
        return Classification("notFound", _rejection(operation, response))
    if status in (408, 409) or status >= 500:
        kind = FailureKind.CONFLICT if status == 409 else FailureKind.AVAILABILITY
        return Classification(
            "retry",
            failure(operation, kind, "Notion read is temporarily unavailable"),
            _retry_after(response),
        )
    if 400 <= status < 500:
        return Classification("rejected", _rejection(operation, response))
    if not 200 <= status < 300:
        return Classification(
            "invalid", failure(operation, FailureKind.PROTOCOL, "Notion returned unexpected status")
        )
    if response.interrupted:
        return Classification(
            "retry", failure(operation, FailureKind.TRANSPORT, "Notion response was interrupted")
        )
    if response.too_large:
        return Classification(
            "invalid",
            failure(
                operation, FailureKind.RESPONSE_TOO_LARGE, "Notion response exceeds the configured size limit"
            ),
        )
    if response.reflects_secret:
        return Classification(
            "invalid",
            failure(operation, FailureKind.PROTOCOL, "Notion response contains credential material"),
        )
    return Classification("success")


def classify_write(operation: str, response: ProviderResponse, repeatable: bool) -> Classification:
    """Classify a nonrepeatable create or an absolute repeatable update."""
    if response.not_built:
        return Classification(
            "defect", failure(operation, FailureKind.LOCAL_DEFECT, "Notion request could not be built")
        )
    if response.error:
        if repeatable or response.error.provably_not_sent:
            return Classification(
                "retry", failure(operation, FailureKind.TRANSPORT, "Notion write is temporarily unavailable")
            )
        return Classification(
            "uncertain", failure(operation, FailureKind.TRANSPORT, "Notion write outcome is unknown")
        )
    status = response.status
    if 200 <= status < 300:
        if response.interrupted:
            return Classification(
                "retry" if repeatable else "uncertain",
                failure(operation, FailureKind.TRANSPORT, "Notion response was interrupted"),
            )
        if response.too_large:
            return Classification(
                "invalid" if repeatable else "uncertain",
                failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Notion response exceeds the configured size limit",
                ),
            )
        if response.reflects_secret:
            return Classification(
                "invalid" if repeatable else "uncertain",
                failure(operation, FailureKind.PROTOCOL, "Notion response contains credential material"),
            )
        return Classification("accepted")
    if (
        status == 429
        and _token(response.body, "/additional_data/rate_limit_reason") == "public_api_request_blocked"
    ):
        return Classification(
            "rejected", failure(operation, FailureKind.PROVIDER_REJECTION, "Notion blocked this connection")
        )
    if status in (429, 529) or status == 409:
        kind = FailureKind.RATE_LIMIT if status != 409 else FailureKind.CONFLICT
        return Classification(
            "retry",
            failure(operation, kind, "Notion write is temporarily unavailable"),
            _retry_after(response),
        )
    if status == 503 and not repeatable:
        from .provider_models import parse_id

        committed = _token(response.body, "/additional_data/committed_resource_id")
        try:
            return Classification("committed", committed_id=parse_id(committed))
        except ValueError:
            pass
    if repeatable and (status == 408 or status >= 500):
        return Classification(
            "retry",
            failure(operation, FailureKind.AVAILABILITY, "Notion write is temporarily unavailable"),
            _retry_after(response),
        )
    if status == 404:
        return Classification("notFound", _rejection(operation, response))
    if 400 <= status < 500 and status != 408:
        return Classification("rejected", _rejection(operation, response))
    if repeatable:
        return Classification(
            "invalid", failure(operation, FailureKind.PROTOCOL, "Notion returned unexpected status")
        )
    return Classification(
        "uncertain", failure(operation, FailureKind.AVAILABILITY, "Notion write outcome is unknown")
    )


def escaped_path(identifier: str) -> str:
    """Escape a path segment like Go url.PathEscape."""
    return quote(identifier, safe="-._~$&+:=@")
