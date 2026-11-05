# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Google Workspace Directory API transport and provider classification."""

import json
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any, cast

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    CredentialRefreshDriver,
    Failure,
    FailureKind,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
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
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

if TYPE_CHECKING:
    from .operations import (
        AddUserToGroupOperation,
        CreateUserOperation,
        GetUserOperation,
        ListUsersOperation,
        RemoveUserFromGroupOperation,
        SuspendUserOperation,
        UnsuspendUserOperation,
    )

_PROVIDER = "google-workspace-admin"
_REPORTABLE = frozenset(
    (
        "badRequest",
        "invalid",
        "required",
        "notFound",
        "duplicate",
        "conflict",
        "conditionNotMet",
        "failedPrecondition",
        "forbidden",
        "insufficientPermissions",
        "authError",
        "limitExceeded",
        "backendError",
        "userRateLimitExceeded",
        "rateLimitExceeded",
        "quotaExceeded",
    )
)
_RATE_REASONS = frozenset(("userRateLimitExceeded", "rateLimitExceeded", "quotaExceeded"))


def _query_escape(value: str) -> str:
    """Encode one query component with Go url.QueryEscape's exact safe set."""
    result = []
    for byte in value.encode("utf-8"):
        if (65 <= byte <= 90) or (97 <= byte <= 122) or (48 <= byte <= 57) or byte in (45, 46, 95):
            result.append(chr(byte))
        elif byte == 32:
            result.append("+")
        else:
            result.append(f"%{byte:02X}")
    return "".join(result)


def query_string(values: dict[str, str]) -> str:
    """Sort keys and encode one value per key like Go url.Values.Encode."""
    return "&".join(f"{_query_escape(key)}={_query_escape(values[key])}" for key in sorted(values))


def path_escape(value: str) -> str:
    """Encode one path segment like Go url.PathEscape."""
    safe = b"ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789-._~$&+=:@"
    return "".join(chr(byte) if byte in safe else f"%{byte:02X}" for byte in value.encode())


@dataclass(frozen=True, slots=True)
class Exchange:
    """A classified Directory exchange with safe response metadata."""

    outcome: str
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    kind: FailureKind | None = None
    message: str = ""
    retry_after: float = 0.0

    def failure(self, operation: str) -> Failure:
        """Return one provider-safe failure for this exchange."""
        assert self.kind is not None
        return Failure(self.kind, _PROVIDER, operation, self.message)

    def receipt(self, call: Call, object_id: str = "") -> Receipt:
        """Return the response receipt, omitting one for local defects."""
        if self.outcome == "defect":
            return Receipt()
        request_id = self.headers.get("X-Goog-Request-Id") or self.headers.get("X-Request-Id", "")
        return Receipt(call.id, _PROVIDER, datetime.now(UTC), call.idempotency_key, object_id, request_id)


def _reason(body: bytes) -> tuple[str, bool]:
    try:
        envelope = json.loads(body)
        if not isinstance(envelope, dict) or not isinstance(envelope.get("error"), dict):
            return "", False
        error = envelope["error"]
        details = error.get("errors")
        reason = ""
        incomplete = (
            "User creation is not complete" in error.get("message", "")
            if isinstance(error.get("message"), str)
            else False
        )
        if isinstance(details, list):
            for detail in details:
                if isinstance(detail, dict):
                    if not reason and isinstance(detail.get("reason"), str):
                        reason = detail["reason"]
                    if (
                        isinstance(detail.get("message"), str)
                        and "User creation is not complete" in detail["message"]
                    ):
                        incomplete = True
        if not reason and isinstance(error.get("status"), str):
            reason = error["status"]
        return reason, incomplete
    except (ValueError, TypeError):
        return "", False


def _retry_after(headers: httpx.Headers) -> float:
    return parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))


def _classify(status: int, headers: httpx.Headers, body: bytes) -> Exchange:
    reason, incomplete = _reason(body)

    def described(message: str) -> str:
        return f"{message} ({reason})" if reason in _REPORTABLE else message

    if incomplete:
        return Exchange(
            "retry",
            status,
            headers,
            body,
            FailureKind.AVAILABILITY,
            "Google is still creating the account",
            _retry_after(headers),
        )
    if status == 429 or status == 403 and reason in _RATE_REASONS:
        return Exchange(
            "retry",
            status,
            headers,
            body,
            FailureKind.RATE_LIMIT,
            described("provider rate limited the request"),
            _retry_after(headers),
        )
    if status == 408 or status >= 500:
        return Exchange(
            "retry",
            status,
            headers,
            body,
            FailureKind.AVAILABILITY,
            described("provider is temporarily unavailable"),
            _retry_after(headers),
        )
    if status == 404:
        return Exchange("notFound", status, headers, body, FailureKind.NOT_FOUND, "resource was not found")
    if status == 409:
        return Exchange(
            "conflict",
            status,
            headers,
            body,
            FailureKind.CONFLICT,
            described("provider reported a conflicting resource"),
        )
    kind = {
        400: FailureKind.VALIDATION,
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        412: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    return Exchange(
        "rejected",
        status,
        headers,
        body,
        kind,
        described(f"provider rejected the request with HTTP {status}"),
    )


class Client:
    """Use call-time renewable credentials for bounded Directory API requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Google Workspace Admin configuration and credentials are required")
        self.endpoint = validate_base_url(config.endpoint or Config().endpoint) + "/admin/directory/v1"
        response_limit = config.max_response_bytes or Config().max_response_bytes
        page_size = config.list_users_page_size or Config().list_users_page_size
        if response_limit < 1 or not 1 <= page_size <= 500:
            raise ValueError("Google Workspace Admin response limit or page size is invalid")
        self.max_response_bytes = response_limit
        self.list_users_page_size = page_size
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        generated_driver = getattr(credentials, "driver", None)
        self.refresh_driver: CredentialRefreshDriver[Credentials] = (
            cast(CredentialRefreshDriver[Credentials], generated_driver)
            if generated_driver is not None
            else ManifestCredentialRefreshDriver[Credentials](
                "Google Workspace Admin", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client, settings={}
            )
        )

    async def aclose(self) -> None:
        """Close the HTTP client transferred to this provider at construction."""
        await self.http_client.aclose()

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve and validate bearer material without exposing secrets."""
        credential = await resolve_credential(self.credentials, call, self.refresh_driver)
        if not is_header_safe_credential(
            credential.access_token.reveal()
        ) or credential.auth_method_id not in ("", "workspace-domain-delegation", "google-oauth"):
            raise ValueError("Google Workspace Admin connection credentials are unavailable")
        return credential

    async def exchange(
        self,
        call: Call,
        credential: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, Any] | None = None,
    ) -> Exchange:
        """Send one request and retry a rejected credential once when supported."""
        target = self.endpoint + path
        if query:
            target += "?" + query_string(query)
        body = (
            json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
            if payload is not None
            else None
        )
        for attempt in range(2):
            headers = {
                "Authorization": "Bearer " + credential.access_token.reveal(),
                "Accept": "application/json",
            }
            if body is not None:
                headers["Content-Type"] = "application/json"
            try:
                request = self.http_client.build_request(method, target, headers=headers, content=body)
                response, _ = await send_traced(self.http_client, request)
            except (ProviderTransportError, httpx.HTTPError, ValueError):
                return Exchange("retry", kind=FailureKind.TRANSPORT, message="provider is unavailable")
            status, response_headers = response.status_code, response.headers
            try:
                data = await read_bounded_body(response, max(self.max_response_bytes, 65536))
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return Exchange(
                        "invalidResponse",
                        status,
                        response_headers,
                        kind=FailureKind.RESPONSE_TOO_LARGE,
                        message="provider response exceeds the configured limit",
                    )
                data = b""
            except ProviderTransportError:
                return Exchange(
                    "retry",
                    status,
                    response_headers,
                    kind=FailureKind.TRANSPORT,
                    message="provider is unavailable",
                )
            if 200 <= status < 300 and len(data) > self.max_response_bytes:
                return Exchange(
                    "invalidResponse",
                    status,
                    response_headers,
                    kind=FailureKind.RESPONSE_TOO_LARGE,
                    message="provider response exceeds the configured limit",
                )
            if (
                status == 401
                and attempt == 0
                and isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                try:
                    credential = await resolve_credential_after_rejection(
                        self.credentials, call, self.refresh_driver
                    )
                    if not is_header_safe_credential(credential.access_token.reveal()):
                        raise ValueError("invalid refreshed credential")
                    continue
                except Exception:
                    # A failed forced refresh leaves the original 401 for status classification.
                    pass
            if 200 <= status < 300:
                return Exchange("success", status, response_headers, data)
            return _classify(status, response_headers, data)
        raise RuntimeError("authenticated request retry was exhausted")

    def get_user(self) -> "GetUserOperation":
        from .operations import GetUserOperation

        return GetUserOperation(self)

    def list_users(self) -> "ListUsersOperation":
        from .operations import ListUsersOperation

        return ListUsersOperation(self)

    def create_user(self) -> "CreateUserOperation":
        from .operations import CreateUserOperation

        return CreateUserOperation(self)

    def suspend_user(self) -> "SuspendUserOperation":
        from .operations import SuspendUserOperation

        return SuspendUserOperation(self)

    def unsuspend_user(self) -> "UnsuspendUserOperation":
        from .operations import UnsuspendUserOperation

        return UnsuspendUserOperation(self)

    def add_user_to_group(self) -> "AddUserToGroupOperation":
        from .operations import AddUserToGroupOperation

        return AddUserToGroupOperation(self)

    def remove_user_from_group(self) -> "RemoveUserFromGroupOperation":
        from .operations import RemoveUserFromGroupOperation

        return RemoveUserFromGroupOperation(self)
