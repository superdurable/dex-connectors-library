# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""RongCloud online-presence Query and signed HTTP transport."""

import hashlib
import json
import os
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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CHECK_USER_ONLINE_DEFINITION
from .models import CheckUserOnlineInput, CheckUserOnlineOutput


def _valid_credential(value: str) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


class Client:
    """Own immutable configuration and resolve current credentials for each request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        random_bytes: Callable[[int], bytes] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("RongCloud configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or not parsed.netloc
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.username
            or parsed.password
        ):
            raise ValueError("RongCloud base URL must be an HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock = clock or (lambda: datetime.now(UTC))
        self.random_bytes = random_bytes or os.urandom

    def check_user_online(self) -> "CheckUserOnlineOperation":
        """Bind the checkUserOnline Query to this client."""
        return CheckUserOnlineOperation(self)

    async def aclose(self) -> None:
        """Close only a transport owned by this client."""
        if self._owns_http_client:
            await self.http_client.aclose()


class CheckUserOnlineOperation:
    """Read one RongCloud IM user's presence."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return CHECK_USER_ONLINE_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[CheckUserOnlineOutput]:
        return QueryAttempt.branch_result(
            branch,
            CheckUserOnlineOutput(),
            Receipt(),
            Failure(kind, "rongcloud", "checkUserOnline", "RongCloud presence query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[CheckUserOnlineOutput]:
        return QueryAttempt.retry(
            Failure(kind, "rongcloud", "checkUserOnline", "RongCloud request failed"), delay
        )

    async def invoke(self, call: Call, input: CheckUserOnlineInput) -> QueryAttempt[CheckUserOnlineOutput]:
        """Sign a form request and classify RongCloud's response."""
        if (
            not isinstance(input, CheckUserOnlineInput)
            or not input.user_id
            or len(input.user_id) > 64
            or not input.user_id.isascii()
            or not input.user_id.isalnum()
        ):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key
            app_secret = credentials.app_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _valid_credential(app_key) or not _valid_credential(app_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        try:
            nonce = self.client.random_bytes(9).hex()
        except (ValueError, OSError):
            return self._branch("defect", FailureKind.AVAILABILITY)
        timestamp = str(int(self.client.clock().timestamp() * 1000))
        signature = hashlib.sha1((app_secret + nonce + timestamp).encode()).hexdigest()
        request = self.client.http_client.build_request(
            "POST",
            self.client.config.base_url.rstrip("/") + "/user/checkOnline.json",
            data={"userId": input.user_id},
            headers={
                "Accept": "application/json",
                "Content-Type": "application/x-www-form-urlencoded",
                "App-Key": app_key,
                "Nonce": nonce,
                "Timestamp": timestamp,
                "Signature": signature,
            },
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), int)
            or isinstance(wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        code = wire["code"]
        if code != 200:
            if code in (1008, 20607):
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if code == 1050:
                return self._retry(FailureKind.AVAILABILITY, delay)
            kind = FailureKind.AUTHENTICATION if code in (1001, 1004) else FailureKind.PROVIDER_REJECTION
            return self._branch("providerRejected", kind)
        status_value = wire.get("status")
        if status_value not in ("0", "1"):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("checked", CheckUserOnlineOutput(status_value == "1"), Receipt())
