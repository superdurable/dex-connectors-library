# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""HubSpot HTTP transport, credential resolution, and response classification."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.credentials import ReauthorizationRequiredError
from dex_connectors.sdk.projectconfig.provider import RefreshFailedError
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

if TYPE_CHECKING:
    from .operations import (
        GetObjectOperation,
        SearchObjectsOperation,
        UpdateObjectOperation,
        UpsertObjectOperation,
    )

_CORRELATION_ID = re.compile(r"[A-Za-z0-9-]{1,64}\Z")
_ERROR_CODE = re.compile(r"[A-Z][A-Z0-9_]{1,63}\Z")
_KNOWN_CATEGORIES = frozenset(
    {
        "BAD_REQUEST",
        "CONFLICT",
        "EXPIRED_AUTHENTICATION",
        "INVALID_AUTHENTICATION",
        "MISSING_SCOPES",
        "OBJECT_ALREADY_EXISTS",
        "OBJECT_NOT_FOUND",
        "RATE_LIMITS",
        "VALIDATION_ERROR",
    }
)


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider response or a classified unsuccessful attempt."""

    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    disposition: str = ""
    failure: Failure | None = None
    retry_after: float = 0


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe HubSpot failure."""
    return Failure(kind, "hubspot", operation, message)


class Client:
    """Execute HubSpot CRM requests using generated OAuth credential wiring.

    A caller-supplied transport remains caller owned. The project connection
    supplies the same transport to generated token refresh and provider calls.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("HubSpot configuration and credential provider are required")
        endpoint = validate_base_url(config.endpoint or Config().endpoint)
        if config.max_response_bytes < 0:
            raise ValueError("HubSpot maxResponseBytes must be positive")
        self.config = Config(endpoint, config.max_response_bytes or Config().max_response_bytes)
        self.endpoint = endpoint.rstrip("/")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close an internally created provider transport."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def search_objects(self) -> "SearchObjectsOperation":
        """Return the CRM search Query."""
        from .operations import SearchObjectsOperation

        return SearchObjectsOperation(self)

    def get_object(self) -> "GetObjectOperation":
        """Return the CRM record read Query."""
        from .operations import GetObjectOperation

        return GetObjectOperation(self)

    def upsert_object(self) -> "UpsertObjectOperation":
        """Return the CRM unique-property upsert Mutation."""
        from .operations import UpsertObjectOperation

        return UpsertObjectOperation(self)

    def update_object(self) -> "UpdateObjectOperation":
        """Return the CRM property update Mutation."""
        from .operations import UpdateObjectOperation

        return UpdateObjectOperation(self)

    def receipt(self, call: Call, exchange: Exchange | None = None, object_id: str = "") -> Receipt:
        """Build a receipt containing only safe provider identity fields."""
        correlation = (
            exchange.headers.get("X-HubSpot-Correlation-Id", "")
            if exchange is not None and exchange.headers is not None
            else ""
        )
        return Receipt(
            call.id,
            "hubspot",
            self.now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            correlation if _CORRELATION_ID.fullmatch(correlation) else "",
        )

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: str = "",
        body: dict[str, Any] | None = None,
    ) -> Exchange:
        """Resolve credentials, send a request, and classify non-success statuses."""
        try:
            credentials = await self.credentials.resolve(call)
            if (
                not isinstance(credentials, Credentials)
                or credentials.access_token is None
                or not is_header_safe_credential(credentials.access_token.reveal())
            ):
                raise ValueError("invalid access token")
        except ReauthorizationRequiredError:
            return Exchange(
                disposition="rejected",
                failure=failure(
                    operation, FailureKind.AUTHENTICATION, "HubSpot authorization must be renewed"
                ),
            )
        except RefreshFailedError:
            return Exchange(
                disposition="retry",
                failure=failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "HubSpot OAuth token refresh is temporarily unavailable",
                ),
            )
        except Exception:
            return Exchange(
                disposition="defect",
                failure=failure(
                    operation, FailureKind.AUTHENTICATION, "HubSpot connection credentials are unavailable"
                ),
            )
        target = f"{self.endpoint}/{path}" + (f"?{query}" if query else "")
        for attempt in range(2):
            try:
                headers = {
                    "Accept": "application/json",
                    "Authorization": f"Bearer {credentials.access_token.reveal()}",
                }
                if body is not None:
                    headers["Content-Type"] = "application/json"
                request = self.http_client.build_request(
                    method,
                    target,
                    headers=headers,
                    content=json.dumps(body, separators=(",", ":"), ensure_ascii=False).encode()
                    if body is not None
                    else None,
                )
                response, _ = await send_traced(self.http_client, request)
                try:
                    contents = await read_bounded_body(response, self.config.max_response_bytes)
                except BodyTooLargeError:
                    return Exchange(
                        response.status_code,
                        response.headers,
                        disposition="invalidResponse",
                        failure=failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "HubSpot response exceeds the configured size limit",
                        ),
                    )
                result = Exchange(response.status_code, response.headers, contents)
            except (ProviderTransportError, httpx.HTTPError, OSError):
                return Exchange(
                    disposition="retry",
                    failure=failure(
                        operation, FailureKind.TRANSPORT, "HubSpot did not return a complete response"
                    ),
                )
            except ValueError:
                return Exchange(
                    disposition="defect",
                    failure=failure(
                        operation, FailureKind.LOCAL_DEFECT, "HubSpot request could not be built"
                    ),
                )
            if (
                result.status == 401
                and attempt == 0
                and credentials.auth_method_id == "hubspot-oauth"
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    replacement = await self.credentials.resolve_after_rejection(call)
                    if replacement.access_token is not None and is_header_safe_credential(
                        replacement.access_token.reveal()
                    ):
                        credentials = replacement
                        continue
                except Exception:
                    pass
            return classify_response(operation, result, self.now())
        return result


def classify_response(operation: str, response: Exchange, now: datetime) -> Exchange:
    """Map HubSpot status and safe error fields to a branch class or Retry."""
    status = response.status
    if 200 <= status < 300:
        return response
    error: dict[str, Any] = {}
    if len(response.body) <= 65536:
        try:
            parsed = json.loads(response.body)
            if isinstance(parsed, dict):
                error = parsed
        except (ValueError, UnicodeDecodeError):
            pass
    category = error.get("category") if isinstance(error.get("category"), str) else ""
    policy = error.get("policyName") if isinstance(error.get("policyName"), str) else ""
    codes = error.get("errors")
    code = codes[0].get("code") if isinstance(codes, list) and codes and isinstance(codes[0], dict) else ""
    details = [category] if category in _KNOWN_CATEGORIES else []
    if isinstance(code, str) and _ERROR_CODE.fullmatch(code) and code != category:
        details.append(code)
    detail = " (" + ", ".join(details) + ")" if details else ""
    delay = parse_retry_after(response.headers.get("Retry-After", "") if response.headers else "", now)

    def classified(disposition: str, kind: FailureKind, message: str, retry_after: float = 0) -> Exchange:
        return Exchange(
            status,
            response.headers,
            response.body,
            disposition,
            failure(operation, kind, message),
            retry_after,
        )

    if status == 429 and policy == "DAILY":
        return classified(
            "rejected", FailureKind.QUOTA_EXHAUSTED, "HubSpot's daily API request limit is exhausted"
        )
    if status == 429:
        interval = (
            response.headers.get("X-HubSpot-RateLimit-Interval-Milliseconds", "") if response.headers else ""
        )
        milliseconds = int(interval) if re.fullmatch(r"[+-]?\d+", interval) else 0
        return classified(
            "retry",
            FailureKind.RATE_LIMIT,
            "HubSpot rate limited the request",
            max(delay, min(milliseconds / 1000, 10) if milliseconds > 0 else 0),
        )
    if status == 423:
        return classified(
            "retry",
            FailureKind.RATE_LIMIT,
            "HubSpot locked the records for a high-volume sync",
            max(delay, 2),
        )
    if status in (408, 477) or (status >= 500 and status != 501):
        return classified(
            "retry", FailureKind.AVAILABILITY, f"HubSpot is temporarily unavailable (HTTP {status})", delay
        )
    if status == 401:
        return classified(
            "rejected", FailureKind.AUTHENTICATION, "HubSpot rejected the access token" + detail
        )
    if status == 403:
        return classified(
            "rejected",
            FailureKind.AUTHORIZATION,
            (
                "HubSpot access token lacks a required scope"
                if category == "MISSING_SCOPES"
                else "HubSpot denied access to the record"
            )
            + detail,
        )
    if status == 404:
        return classified("notFound", FailureKind.NOT_FOUND, "HubSpot found no such record" + detail)
    if status == 409:
        return classified(
            "conflict", FailureKind.CONFLICT, "HubSpot reported a conflict with another record" + detail
        )
    if status in (400, 422) or category == "VALIDATION_ERROR":
        return classified(
            "rejected", FailureKind.VALIDATION, "HubSpot rejected the request as invalid" + detail
        )
    if 300 <= status < 400:
        return classified("rejected", FailureKind.PROTOCOL, f"HubSpot redirected the request (HTTP {status})")
    return classified(
        "rejected", FailureKind.PROVIDER_REJECTION, f"HubSpot rejected the request (HTTP {status})" + detail
    )
