# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bilibili authorized-user Query with signed provider requests."""

import hashlib
import hmac
import json
import secrets
import time
from collections.abc import Callable
from datetime import UTC, datetime
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    FOUND,
    GET_AUTHORIZED_USER_DEFINITION,
    GET_AUTHORIZED_USER_OPERATION_ID,
    INVALID_RESPONSE,
    PROVIDER_REJECTED,
)
from .models import AuthorizedUser, GetAuthorizedUserInput

_SIGNED_HEADER_NAMES = (
    "x-bili-accesskeyid",
    "x-bili-content-md5",
    "x-bili-signature-method",
    "x-bili-signature-nonce",
    "x-bili-signature-version",
    "x-bili-timestamp",
)


class Client:
    """Bounded provider client that signs each lookup with current credentials."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        nonce_bytes: Callable[[int], bytes] = secrets.token_bytes,
        unix_time: Callable[[], int] = lambda: int(time.time()),
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Bilibili configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Bilibili timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        parsed = urlsplit(self.base_url)
        if parsed.path or parsed.query or parsed.fragment:
            raise ValueError("Bilibili API URL must be an origin without a path or query")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self.nonce_bytes = nonce_bytes
        self.unix_time = unix_time

    def get_authorized_user(self) -> "GetAuthorizedUserOperation":
        """Bind the authorized-user Query to this client."""
        return GetAuthorizedUserOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class GetAuthorizedUserOperation:
    """Read the user's public nickname and application-scoped open ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Bilibili client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_AUTHORIZED_USER_DEFINITION

    async def invoke(self, call: Call, input: GetAuthorizedUserInput) -> QueryAttempt[AuthorizedUser]:
        """Sign one request and classify provider and transport outcomes."""
        try:
            credentials = await self.client.credentials.resolve(call)
            client_id = credentials.client_id
            app_secret = credentials.app_secret.reveal()
            access_token = credentials.access_token.reveal()
        except Exception:
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Bilibili credentials are unavailable or invalid"
            )
        if not all(_opaque(value) for value in (client_id, app_secret, access_token)):
            return _branch(
                DEFECT, FailureKind.AUTHENTICATION, "Bilibili credentials are unavailable or invalid"
            )
        try:
            nonce = self.client.nonce_bytes(16)
            if not isinstance(nonce, bytes) or len(nonce) != 16:
                raise ValueError("invalid nonce")
            headers = {
                "x-bili-accesskeyid": client_id,
                "x-bili-content-md5": hashlib.md5(b"").hexdigest(),  # noqa: S324
                "x-bili-signature-method": "HMAC-SHA256",
                "x-bili-signature-nonce": nonce.hex(),
                "x-bili-signature-version": "2.0",
                "x-bili-timestamp": str(self.client.unix_time()),
            }
            authorization = _sign_request(app_secret, headers)
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/arcopen/fn/user/account/info",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Access-Token": access_token,
                    "Authorization": authorization,
                    **headers,
                },
            )
        except (httpx.HTTPError, ValueError, TypeError):
            return _retry(FailureKind.TRANSPORT, "Bilibili request failed")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.TRANSPORT, "Bilibili request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Bilibili rate limited the query", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Bilibili is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Bilibili response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.TRANSPORT, "Bilibili response was interrupted")
            if status != 200:
                return _branch(
                    PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Bilibili rejected HTTP {status}"
                )
            try:
                document = json.loads(
                    body.decode("utf-8"),
                    parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid constant")),
                )
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Bilibili returned invalid account JSON"
                )
            if not isinstance(document, dict) or type(document.get("code")) is not int:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "Bilibili returned no status code")
            code = document["code"]
            if code == 0:
                user = document.get("data")
                if (
                    not isinstance(user, dict)
                    or not isinstance(user.get("openid"), str)
                    or not user["openid"]
                    or not isinstance(user.get("name"), str)
                    or not user["name"]
                ):
                    return _branch(
                        INVALID_RESPONSE, FailureKind.PROTOCOL, "Bilibili returned incomplete identity"
                    )
                return QueryAttempt.branch_result(
                    FOUND, AuthorizedUser(user["openid"], user["name"]), Receipt()
                )
            if code in (127009, 127306):
                return _retry(FailureKind.RATE_LIMIT, "Bilibili rate limited the query", retry_after)
            if code in (4010, 4011, 122009, 122010, 123002, 127039):
                return _retry(FailureKind.AVAILABILITY, "Bilibili is temporarily unavailable", retry_after)
            return _branch(
                PROVIDER_REJECTED, FailureKind.PROVIDER_REJECTION, f"Bilibili rejected code {code}"
            )
        finally:
            await response.aclose()


def _opaque(value: object) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 4096
        and all("!" <= character <= "~" for character in value)
    )


def _sign_request(secret: str, headers: dict[str, str]) -> str:
    canonical = "\n".join(f"{name}:{headers[name]}" for name in _SIGNED_HEADER_NAMES)
    return hmac.new(secret.encode(), canonical.encode(), hashlib.sha256).hexdigest()


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AuthorizedUser]:
    return QueryAttempt.branch_result(
        branch,
        AuthorizedUser(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, GET_AUTHORIZED_USER_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, after_seconds: float = 0) -> QueryAttempt[AuthorizedUser]:
    return QueryAttempt.retry(
        Failure(kind, CONNECTOR_ID, GET_AUTHORIZED_USER_OPERATION_ID, message), after_seconds
    )


Config.__doc__ = "Bilibili Open Platform HTTPS origin, timeout in seconds, and response bound."
Credentials.__doc__ = "Private app secret and access token plus public client ID resolved per Query."
