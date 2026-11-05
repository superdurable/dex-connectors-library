# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Pipedrive transport, credential resolution, and provider status classification."""

import ipaddress
import math
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
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

if TYPE_CHECKING:
    from .operations import (
        CreateObjectOperation,
        GetObjectOperation,
        ListObjectsOperation,
        SearchObjectsOperation,
        UpdateObjectOperation,
        UpsertObjectOperation,
    )


@dataclass(slots=True)
class Exchange:
    """One classified provider exchange, including safe response metadata."""

    outcome: str
    failure: Failure | None = None
    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    retry_after: float = 0


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a Pipedrive failure without provider response data."""
    return Failure(kind, "pipedrive", operation, message)


def _base_url(value: str) -> str:
    url = validate_base_url(value)
    parsed = urlsplit(url)
    if parsed.path:
        raise ValueError("base URL must not contain a path")
    host = (parsed.hostname or "").lower()
    try:
        is_loopback = ipaddress.ip_address(host).is_loopback
    except ValueError:
        is_loopback = host == "localhost"
    if host != "pipedrive.com" and not host.endswith(".pipedrive.com") and not is_loopback:
        raise ValueError("base URL must be a pipedrive.com host")
    return url


class Client:
    """Send Pipedrive requests with a call-time credential source.

    An injected HTTP client remains caller owned; close a connector-owned client
    through ``aclose`` after the Worker stops.
    """

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Pipedrive configuration and credentials are required")
        self.endpoint = _base_url(config.endpoint or Config().endpoint)
        self.max_response_bytes = config.max_response_bytes or Config().max_response_bytes
        if self.max_response_bytes <= 0:
            raise ValueError("Pipedrive maxResponseBytes must be positive")
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(20)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    async def aclose(self) -> None:
        """Close the connector-owned provider transport."""
        if self._owns_client:
            await self.http_client.aclose()

    def search_objects(self) -> "SearchObjectsOperation":
        """Bind the search Query."""
        from .operations import SearchObjectsOperation

        return SearchObjectsOperation(self)

    def get_object(self) -> "GetObjectOperation":
        """Bind the record read Query."""
        from .operations import GetObjectOperation

        return GetObjectOperation(self)

    def list_objects(self) -> "ListObjectsOperation":
        """Bind the list Query."""
        from .operations import ListObjectsOperation

        return ListObjectsOperation(self)

    def upsert_object(self) -> "UpsertObjectOperation":
        """Bind the identity upsert Mutation."""
        from .operations import UpsertObjectOperation

        return UpsertObjectOperation(self)

    def create_object(self) -> "CreateObjectOperation":
        """Bind the record create Mutation."""
        from .operations import CreateObjectOperation

        return CreateObjectOperation(self)

    def update_object(self) -> "UpdateObjectOperation":
        """Bind the record update Mutation."""
        from .operations import UpdateObjectOperation

        return UpdateObjectOperation(self)

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, Exchange | None]:
        """Resolve credentials and distinguish terminal from transient failure."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            if isinstance(error, ReauthorizationRequiredError):
                return None, Exchange(
                    "rejected",
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "Pipedrive authorization must be renewed; reconnect the connection",
                    ),
                )
            if str(error) == "connection is not configured":
                return None, Exchange(
                    "defect",
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "Pipedrive connection credentials are unavailable; reconnect the connection",
                    ),
                )
            return None, Exchange(
                "retry",
                failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Pipedrive connection credentials are temporarily unavailable",
                ),
            )
        if not isinstance(credentials, Credentials) or credentials.auth_method_id not in (
            "api-token",
            "pipedrive-oauth",
        ):
            return None, Exchange(
                "defect",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Pipedrive connection credentials are unavailable; reconnect the connection",
                ),
            )
        token = self._token(credentials)
        if not is_header_safe_credential(token):
            return None, Exchange(
                "defect",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Pipedrive connection credentials are unavailable; reconnect the connection",
                ),
            )
        if credentials.auth_method_id == "pipedrive-oauth":
            try:
                _base_url(credentials.api_domain)
            except ValueError:
                return None, Exchange(
                    "defect",
                    failure(
                        operation,
                        FailureKind.AUTHENTICATION,
                        "Pipedrive OAuth connection has no valid api_domain; reconnect the connection",
                    ),
                )
        return credentials, None

    @staticmethod
    def _token(credentials: Credentials) -> str:
        secret = (
            credentials.access_token
            if credentials.auth_method_id == "pipedrive-oauth"
            else credentials.api_token
        )
        return secret.reveal() if secret is not None else ""

    async def exchange(
        self,
        call: Call,
        operation: str,
        credentials: Credentials,
        method: str,
        path: str,
        *,
        query: str = "",
        body: bytes | None = None,
        allow_refresh: bool = True,
    ) -> Exchange:
        """Send one API exchange and refresh an OAuth credential once after 401."""
        result = await self._exchange_once(call, operation, credentials, method, path, query, body)
        if (
            result.status == 401
            and allow_refresh
            and credentials.auth_method_id == "pipedrive-oauth"
            and hasattr(self.credentials, "resolve_after_rejection")
        ):
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                if (
                    isinstance(replacement, Credentials)
                    and self._token(replacement) != self._token(credentials)
                    and is_header_safe_credential(self._token(replacement))
                ):
                    return await self._exchange_once(call, operation, replacement, method, path, query, body)
            except Exception:
                pass
        return result

    async def refresh_after_write_rejection(
        self, call: Call, credentials: Credentials, operation: str
    ) -> tuple[bool, Failure]:
        """Ask generated OAuth wiring for a replacement after a rejected write."""
        reconnect = "Pipedrive rejected the access token (HTTP 401); reconnect the connection"
        temporary = (
            "Pipedrive rejected the access token (HTTP 401); credential refresh is temporarily unavailable"
        )
        if not hasattr(self.credentials, "resolve_after_rejection"):
            return False, failure(operation, FailureKind.AUTHENTICATION, reconnect)
        try:
            replacement = await self.credentials.resolve_after_rejection(call)
        except ReauthorizationRequiredError:
            return False, failure(operation, FailureKind.AUTHENTICATION, reconnect)
        except Exception:
            return False, failure(operation, FailureKind.AUTHENTICATION, temporary)
        if (
            not isinstance(replacement, Credentials)
            or replacement.auth_method_id != "pipedrive-oauth"
            or self._token(replacement) == self._token(credentials)
            or not is_header_safe_credential(self._token(replacement))
        ):
            return False, failure(operation, FailureKind.AUTHENTICATION, reconnect)
        try:
            _base_url(replacement.api_domain)
        except ValueError:
            return False, failure(operation, FailureKind.AUTHENTICATION, reconnect)
        return True, failure(operation, FailureKind.AUTHENTICATION, reconnect)

    async def _exchange_once(
        self,
        call: Call,
        operation: str,
        credentials: Credentials,
        method: str,
        path: str,
        query: str,
        body: bytes | None,
    ) -> Exchange:
        token = self._token(credentials)
        base = (
            self.endpoint
            if credentials.auth_method_id != "pipedrive-oauth"
            else _base_url(credentials.api_domain)
        )
        target = base + path + ("?" + query if query else "")
        headers = {"Accept": "application/json"}
        if credentials.auth_method_id == "pipedrive-oauth":
            headers["Authorization"] = "Bearer " + token
        else:
            headers["X-Api-Token"] = token
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, target, headers=headers, content=body, timeout=20
            )
            response, _ = await send_traced(self.http_client, request)
            status, response_headers = response.status_code, response.headers
            try:
                content = await read_bounded_body(
                    response, self.max_response_bytes if 200 <= status < 300 else 65536
                )
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return Exchange(
                        "invalid",
                        failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Pipedrive response exceeds the configured maxResponseBytes limit",
                        ),
                        status,
                        response_headers,
                    )
                content = b""
            except ProviderTransportError:
                return Exchange(
                    "unavailable",
                    failure(operation, FailureKind.TRANSPORT, "Pipedrive response could not be read"),
                    status,
                    response_headers,
                )
        except ProviderTransportError as error:
            if error.provably_not_sent:
                return Exchange(
                    "notSent",
                    failure(
                        operation,
                        FailureKind.TRANSPORT,
                        "Pipedrive could not be reached; no request was sent",
                    ),
                )
            return Exchange(
                "unavailable",
                failure(
                    operation, FailureKind.TRANSPORT, "Pipedrive request failed before a response arrived"
                ),
            )
        except httpx.HTTPError:
            return Exchange(
                "unavailable",
                failure(
                    operation, FailureKind.TRANSPORT, "Pipedrive request failed before a response arrived"
                ),
            )
        if token.encode() in content:
            if 200 <= status < 300:
                return Exchange(
                    "invalid",
                    failure(
                        operation,
                        FailureKind.PROTOCOL,
                        "Pipedrive response reflected the connection credential",
                    ),
                    status,
                    response_headers,
                )
            content = b""
        if 200 <= status < 300:
            return Exchange("succeeded", status=status, headers=response_headers, body=content)
        return self._classify(operation, status, response_headers)

    def _classify(self, operation: str, status: int, headers: httpx.Headers) -> Exchange:
        delay = parse_retry_after(headers.get("Retry-After", ""), self.now())

        def result(outcome: str, kind: FailureKind, message: str, retry_after: float = 0) -> Exchange:
            return Exchange(
                outcome, failure(operation, kind, message), status, headers, retry_after=retry_after
            )

        if status == 429:
            remaining = headers.get("X-Daily-Ratelimit-Token-Remaining", "").strip()
            if re.fullmatch(r"[+-]?[0-9]+", remaining) and -(2**63) <= int(remaining) <= 0:
                return result(
                    "rejected",
                    FailureKind.QUOTA_EXHAUSTED,
                    (
                        "Pipedrive's daily API token budget is exhausted until it resets "
                        "at midnight in Pipedrive's server time zone"
                    ),
                )
            try:
                reset = float(headers.get("X-Ratelimit-Reset", ""))
            except ValueError:
                reset = 0
            if not math.isfinite(reset):
                reset = 0
            return result(
                "rateLimited",
                FailureKind.RATE_LIMIT,
                "Pipedrive's burst rate limit rejected the request",
                max(delay, min(max(reset, 2), 10)),
            )
        if status in (408, 409) or status >= 500 and status != 501:
            return result(
                "unavailable",
                FailureKind.AVAILABILITY,
                f"Pipedrive is temporarily unavailable (HTTP {status})",
                delay,
            )
        fixed = {
            401: (
                "rejected",
                FailureKind.AUTHENTICATION,
                "Pipedrive rejected the API token or access token (HTTP 401)",
            ),
            402: (
                "rejected",
                FailureKind.PROVIDER_REJECTION,
                "Pipedrive requires a payment or plan change for the company account (HTTP 402)",
            ),
            403: (
                "rejected",
                FailureKind.AUTHORIZATION,
                (
                    "Pipedrive denied the request, such as a missing scope or permission, "
                    "or blocked API traffic (HTTP 403)"
                ),
            ),
            404: ("notFound", FailureKind.NOT_FOUND, "Pipedrive found no such record (HTTP 404)"),
        }
        if status in fixed:
            outcome, kind, message = fixed[status]
            return result(outcome, kind, message)
        if status in (400, 422):
            return result(
                "rejected",
                FailureKind.VALIDATION,
                f"Pipedrive rejected the request as invalid (HTTP {status})",
            )
        if 300 <= status < 400:
            return result(
                "rejected", FailureKind.PROTOCOL, f"Pipedrive redirected the request (HTTP {status})"
            )
        return result(
            "rejected", FailureKind.PROVIDER_REJECTION, f"Pipedrive rejected the request (HTTP {status})"
        )

    def receipt(self, call: Call, result: Exchange, object_id: str = "") -> Receipt:
        """Build one safe provider receipt with the Go correlation fields."""
        request_id = result.headers.get("X-Correlation-Id", "") if result.headers else ""
        if not re.fullmatch(r"[A-Za-z0-9-]{1,64}", request_id):
            request_id = ""
        return Receipt(call.id, "pipedrive", self.now(), call.idempotency_key, object_id, request_id)
