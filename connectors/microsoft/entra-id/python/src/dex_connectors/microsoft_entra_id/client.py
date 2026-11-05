# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Microsoft Graph client with call-time renewable credentials."""

from __future__ import annotations

import ipaddress
import json
from dataclasses import dataclass, replace
from datetime import UTC, datetime, timedelta
from typing import Any
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
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
from .models import User

GRAPH_BASE = "https://graph.microsoft.com/v1.0"
USER_SELECT = (
    "id,userPrincipalName,displayName,givenName,surname,mail,mailNickname,accountEnabled,"
    "userType,jobTitle,department,usageLocation,onPremisesSyncEnabled,createdDateTime,"
    "signInSessionsValidFromDateTime"
)
CREATION_SELECT = USER_SELECT + ",onPremisesExtensionAttributes"
_SAFE_PATH = "-._~$&+=:@"
_REPORTABLE_CODES = frozenset(
    (
        "Request_BadRequest",
        "Request_ResourceNotFound",
        "Request_UnsupportedQuery",
        "Authorization_RequestDenied",
        "Authorization_IdentityNotFound",
        "InvalidAuthenticationToken",
        "Directory_QuotaExceeded",
        "Directory_ConcurrencyViolation",
        "Directory_ResultSizeLimitExceeded",
        "accessDenied",
        "badRequest",
        "invalidRequest",
        "itemNotFound",
        "unauthenticated",
        "serviceNotAvailable",
        "generalException",
        "activityLimitReached",
        "TooManyRequests",
    )
)


def path_escape(value: str) -> str:
    """Escape one path segment like Go url.PathEscape."""
    return quote(value, safe=_SAFE_PATH, encoding="utf-8", errors="strict")


def query_escape(value: str) -> str:
    """Escape a query value like Go QueryEscape, with spaces retained as %20."""
    return quote(value, safe="", encoding="utf-8", errors="strict").replace("~", "%7E")


def odata_query(values: dict[str, str]) -> str:
    """Encode sorted constant OData keys with Go-compatible escaped values."""
    return "&".join(f"{key}={query_escape(values[key])}" for key in sorted(values))


@dataclass(slots=True)
class Exchange:
    """Classified Graph exchange and its safe failure context."""

    outcome: str
    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    failure: Failure | None = None
    retry_after: float = 0


class Client:
    """Execute Microsoft Graph calls with bounded bodies and one 401 refresh."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        local_provider_url: str = "",
        now: Any = None,
        random_source: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Microsoft Entra ID configuration and credentials are required")
        config = replace(
            config,
            max_response_bytes=config.max_response_bytes or Config().max_response_bytes,
            list_users_page_size=config.list_users_page_size or Config().list_users_page_size,
            creation_key_attribute=config.creation_key_attribute or Config().creation_key_attribute,
        )
        if config.max_response_bytes < 1 or not 1 <= config.list_users_page_size <= 999:
            raise ValueError("Microsoft Entra ID response or page limit is invalid")
        if config.creation_key_attribute not in {f"extensionAttribute{index}" for index in range(1, 16)}:
            raise ValueError("Microsoft Entra ID creation key attribute is invalid")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.random_source = random_source
        self.local_provider_url = ""
        if local_provider_url:
            value = validate_base_url(local_provider_url)
            parsed = urlsplit(value)
            try:
                is_loopback = ipaddress.ip_address(parsed.hostname or "").is_loopback
            except ValueError:
                is_loopback = (parsed.hostname or "").lower() == "localhost"
            if not is_loopback or parsed.path:
                raise ValueError("Microsoft Entra ID local provider URL must be loopback without path")
            self.local_provider_url = value

    async def aclose(self) -> None:
        """Close an HTTP client owned by this connector."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def get_user(self) -> Any:
        """Bind the getUser Query."""
        from .operations import GetUserOperation

        return GetUserOperation(self)

    def list_users(self) -> Any:
        """Bind the listUsers Query."""
        from .operations import ListUsersOperation

        return ListUsersOperation(self)

    def create_user(self) -> Any:
        """Bind the createUser Mutation."""
        from .operations import CreateUserOperation

        return CreateUserOperation(self)

    def disable_user(self) -> Any:
        """Bind the disableUser Mutation."""
        from .operations import DisableUserOperation

        return DisableUserOperation(self)

    def enable_user(self) -> Any:
        """Bind the enableUser Mutation."""
        from .operations import EnableUserOperation

        return EnableUserOperation(self)

    def revoke_sign_in_sessions(self) -> Any:
        """Bind the revokeSignInSessions Mutation."""
        from .operations import RevokeSignInSessionsOperation

        return RevokeSignInSessionsOperation(self)

    def add_user_to_group(self) -> Any:
        """Bind the addUserToGroup Mutation."""
        from .operations import AddUserToGroupOperation

        return AddUserToGroupOperation(self)

    def remove_user_from_group(self) -> Any:
        """Bind the removeUserFromGroup Mutation."""
        from .operations import RemoveUserFromGroupOperation

        return RemoveUserFromGroupOperation(self)

    async def resolve_credentials(self, call: Call) -> Credentials:
        """Resolve a current Graph access token through the generated source."""
        credential = await self.credentials.resolve(call)
        if not isinstance(credential, Credentials) or not is_header_safe_credential(
            credential.access_token.reveal()
        ):
            raise ValueError("Microsoft Entra ID access token is unavailable")
        if credential.auth_method_id not in ("", "entra-app-only", "microsoft-oauth"):
            raise ValueError("Microsoft Entra ID authorization method is invalid")
        return credential

    async def exchange(
        self,
        call: Call,
        credential: Credentials,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: object = None,
        eventual: bool = False,
        absolute_url: str = "",
    ) -> Exchange:
        """Send a Graph request and classify its status before operation routing."""
        target = absolute_url or GRAPH_BASE + path + ("?" + odata_query(query) if query else "")
        if self.local_provider_url:
            parsed = urlsplit(target)
            if parsed.scheme != "https" or parsed.netloc != "graph.microsoft.com":
                return Exchange(
                    "defect",
                    failure=failure(
                        operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"
                    ),
                )
            target = self.local_provider_url + parsed.path + ("?" + parsed.query if parsed.query else "")
        try:
            encoded = (
                json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode()
                if payload is not None
                else None
            )
        except (TypeError, ValueError):
            return Exchange(
                "defect",
                failure=failure(operation, FailureKind.LOCAL_DEFECT, "provider request could not be encoded"),
            )
        for attempt in range(2):
            headers = {
                "Authorization": "Bearer " + credential.access_token.reveal(),
                "Accept": "application/json",
            }
            if encoded is not None or method == "POST":
                headers["Content-Type"] = "application/json"
            if eventual:
                headers["ConsistencyLevel"] = "eventual"
            try:
                request = self.http_client.build_request(method, target, headers=headers, content=encoded)
                response, _ = await send_traced(self.http_client, request)
                body = await read_bounded_body(
                    response, max(self.config.max_response_bytes, MAX_ERROR_BODY_BYTES)
                )
                if 200 <= response.status_code < 300 and len(body) > self.config.max_response_bytes:
                    return Exchange(
                        "invalidResponse",
                        response.status_code,
                        response.headers,
                        failure=failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "provider response exceeds the configured limit",
                        ),
                    )
            except BodyTooLargeError as error:
                if 200 <= error.status_code < 300:
                    return Exchange(
                        "invalidResponse",
                        error.status_code,
                        error.headers,
                        failure=failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "provider response exceeds the configured limit",
                        ),
                    )
                response = httpx.Response(error.status_code, headers=error.headers)
                body = b""
            except ProviderTransportError:
                return Exchange(
                    "retry", failure=failure(operation, FailureKind.TRANSPORT, "provider is unavailable")
                )
            except (ValueError, httpx.InvalidURL):
                return Exchange(
                    "defect",
                    failure=failure(
                        operation, FailureKind.LOCAL_DEFECT, "provider request could not be built"
                    ),
                )
            if (
                response.status_code == 401
                and attempt == 0
                and hasattr(self.credentials, "resolve_after_rejection")
            ):
                try:
                    refreshed = await self.credentials.resolve_after_rejection(call)
                    if isinstance(refreshed, Credentials) and is_header_safe_credential(
                        refreshed.access_token.reveal()
                    ):
                        credential = refreshed
                        continue
                except Exception:
                    pass
            return classify(operation, response.status_code, response.headers, body, self.now())
        raise AssertionError("unreachable credential retry")

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Correlate a provider response to the Dex Call without secret material."""
        headers = exchange.headers or httpx.Headers()
        return Receipt(
            call.id,
            "microsoft-entra-id",
            self.now(),
            call.idempotency_key,
            object_id,
            headers.get("request-id") or headers.get("client-request-id", ""),
        )

    def within_propagation_window(self, call: Call) -> bool:
        """Use Dex's first attempt time to bound replication retries."""
        first = getattr(call.context, "first_attempt_at", None)
        return isinstance(first, datetime) and self.now() - first < timedelta(minutes=1)


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a secret-safe Graph failure."""
    return Failure(kind, "microsoft-entra-id", operation, message)


def classify(operation: str, status: int, headers: httpx.Headers, body: bytes, now: datetime) -> Exchange:
    """Map HTTP status and a documented Graph code to a safe outcome."""
    if 200 <= status < 300:
        return Exchange("success", status, headers, body)
    codes = read_error_tokens(body, ("/error/code", "/error/innerError/code", "/error/innererror/code"))
    code = codes[0] if codes else ""
    suffix = f" ({code})" if code in _REPORTABLE_CODES else ""
    if status == 429:
        return Exchange(
            "retry",
            status,
            headers,
            body,
            failure(operation, FailureKind.RATE_LIMIT, "provider throttled the request" + suffix),
            parse_retry_after(headers.get("Retry-After", ""), now),
        )
    if status == 408 or (status >= 500 and status != 501):
        return Exchange(
            "retry",
            status,
            headers,
            body,
            failure(operation, FailureKind.AVAILABILITY, "provider is temporarily unavailable" + suffix),
            parse_retry_after(headers.get("Retry-After", ""), now),
        )
    if status == 409 and code == "Directory_ConcurrencyViolation":
        return Exchange(
            "retry",
            status,
            headers,
            body,
            failure(
                operation,
                FailureKind.AVAILABILITY,
                "provider reported a concurrent directory change" + suffix,
            ),
            parse_retry_after(headers.get("Retry-After", ""), now),
        )
    if status == 404:
        return Exchange(
            "notFound",
            status,
            headers,
            body,
            failure(operation, FailureKind.NOT_FOUND, "resource was not found" + suffix),
        )
    if status == 400:
        return Exchange(
            "badRequest",
            status,
            headers,
            body,
            failure(
                operation, FailureKind.VALIDATION, "provider rejected the request with HTTP 400" + suffix
            ),
        )
    kind = {
        401: FailureKind.AUTHENTICATION,
        403: FailureKind.AUTHORIZATION,
        409: FailureKind.CONFLICT,
        412: FailureKind.CONFLICT,
    }.get(status, FailureKind.PROVIDER_REJECTION)
    return Exchange(
        "rejected",
        status,
        headers,
        body,
        failure(operation, kind, f"provider rejected the request with HTTP {status}" + suffix),
    )


def decode_user(body: bytes, require_enabled: bool = True) -> tuple[User, dict[str, Any]]:
    """Validate one untrusted account and return its bounded model."""
    try:
        source = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        raise ValueError("account is not valid JSON") from None
    if not isinstance(source, dict):
        raise ValueError("account lacks a valid object ID")
    return convert_user(source, require_enabled), source


def convert_user(source: dict[str, Any], require_enabled: bool = True) -> User:
    """Map only Go's declared Graph account fields, preserving strict types."""
    import re

    if not isinstance(source, dict):
        raise ValueError("account lacks a valid object ID")
    attributes = source.get("onPremisesExtensionAttributes")
    if attributes is not None and (
        not isinstance(attributes, dict)
        or any(value is not None and not isinstance(value, str) for value in attributes.values())
    ):
        raise ValueError("account is not valid JSON")
    if not isinstance(source.get("id"), str) or not re.fullmatch(
        r"[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}", source["id"]
    ):
        raise ValueError("account lacks a valid object ID")
    upn = source.get("userPrincipalName")
    if not isinstance(upn, str) or not is_lookup_upn(upn):
        raise ValueError("account lacks a valid user principal name")
    enabled = source.get("accountEnabled")
    if enabled is not None and type(enabled) is not bool:
        raise ValueError("account is not valid JSON")
    if require_enabled and enabled is None:
        raise ValueError("account lacks accountEnabled; the token may lack User.Read.All")
    sync = source.get("onPremisesSyncEnabled")
    if sync is not None and type(sync) is not bool:
        raise ValueError("account is not valid JSON")
    names = {
        "displayName": "display_name",
        "givenName": "given_name",
        "surname": "surname",
        "mail": "mail",
        "mailNickname": "mail_nickname",
        "userType": "user_type",
        "jobTitle": "job_title",
        "department": "department",
        "usageLocation": "usage_location",
        "createdDateTime": "created_date_time",
        "signInSessionsValidFromDateTime": "sign_in_sessions_valid_from_date_time",
    }
    values = {}
    for wire, field in names.items():
        value = source.get(wire)
        if value is not None and not isinstance(value, str):
            raise ValueError("account is not valid JSON")
        values[field] = value or ""
    return User(
        id=source["id"].lower(),
        user_principal_name=upn,
        is_account_enabled=enabled is True,
        is_on_premises_sync_enabled=sync is True,
        **values,
    )


def is_lookup_upn(value: str) -> bool:
    """Accept Go's Microsoft Graph UPN lookup character set."""
    import re

    return (
        len(value) <= 256
        and value.count("@") == 1
        and re.fullmatch(r"[A-Za-z0-9'.\-_!#^~$&+=]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?", value)
        is not None
    )
