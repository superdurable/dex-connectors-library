# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Honor app ID transport and Go-compatible Query decisions."""

import json
import re
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
from ._generated.definitions import LOOKUP_APP_ID_DEFINITION
from .models import LookupAppIDInput, LookupAppIDOutput

_TOKEN_ENDPOINT = "https://iam.developer.honor.com/auth/token"
_LOOKUP_ENDPOINT = "https://appmarket-openapi-drcn.cloud.honor.com/openapi/v1/publish/get-app-id"
_PACKAGE_PATTERN = re.compile(r"^[A-Za-z][A-Za-z0-9_]*(\.[A-Za-z][A-Za-z0-9_]*)+$")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[LookupAppIDOutput]:
    return QueryAttempt.branch_result(
        branch, LookupAppIDOutput(), Receipt(), Failure(kind, "honor", "lookupAppID", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[LookupAppIDOutput]:
    return QueryAttempt.retry(Failure(kind, "honor", "lookupAppID", message), delay)


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the redirect-safe HTTP transport and resolve credentials for every app lookup."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Honor configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("Honor timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def lookup_app_id(self) -> "LookupAppIDOperation":
        """Bind the app ID Query to this client."""
        return LookupAppIDOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class LookupAppIDOperation:
    """Read one owned Android app ID after a short-lived token exchange."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Honor client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated app ID Query identity."""
        return LOOKUP_APP_ID_DEFINITION

    async def invoke(self, call: Call, input: LookupAppIDInput) -> QueryAttempt[LookupAppIDOutput]:
        """Exchange credentials, query the app ID, and classify provider outcomes."""
        if len(input.package_name) > 255 or _PACKAGE_PATTERN.fullmatch(input.package_name) is None:
            return _branch("defect", FailureKind.VALIDATION, "Honor package name is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            client_id = credentials.client_id.reveal()
            client_secret = credentials.client_secret.reveal()
            if not _valid_credential(client_id) or not _valid_credential(client_secret):
                raise ValueError("invalid Honor credentials")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Honor credentials are unavailable or invalid"
            )
        token_request = self.client.http_client.build_request(
            "POST",
            _TOKEN_ENDPOINT,
            data={"grant_type": "client_credentials", "client_id": client_id, "client_secret": client_secret},
            headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
        )
        token_body, decision = await self._send_request(token_request)
        if decision is not None:
            return decision
        assert token_body is not None
        token, decision = self._decode_token(token_body)
        if decision is not None:
            return decision
        app_request = self.client.http_client.build_request(
            "GET",
            _LOOKUP_ENDPOINT,
            params={"pkgName": input.package_name},
            headers={"Authorization": "Bearer " + token, "Accept": "application/json"},
        )
        app_body, decision = await self._send_request(app_request)
        if decision is not None:
            return decision
        assert app_body is not None
        return self._decode_app(app_body, input.package_name)

    async def _send_request(
        self, request: httpx.Request
    ) -> tuple[bytes, None] | tuple[None, QueryAttempt[LookupAppIDOutput]]:
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return None, _retry(FailureKind.AVAILABILITY, "Honor request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return None, _retry(FailureKind.RATE_LIMIT, "Honor rate limited the request", delay)
            if status == 408 or status >= 500:
                return None, _retry(FailureKind.AVAILABILITY, "Honor API is temporarily unavailable", delay)
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return None, _branch("providerRejected", kind, f"Honor rejected request with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return None, _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Honor response exceeded the configured limit",
                )
            except ProviderTransportError:
                return None, _retry(FailureKind.AVAILABILITY, "Honor response was interrupted")
        finally:
            await response.aclose()
        return body, None

    def _decode_token(self, body: bytes) -> tuple[str, None] | tuple[str, QueryAttempt[LookupAppIDOutput]]:
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            document = None
        if (
            not isinstance(document, dict)
            or not isinstance(document.get("access_token"), str)
            or not _valid_credential(document["access_token"])
            or type(document.get("expires_in")) is not int
            or document["expires_in"] <= 0
            or document.get("token_type") != "Bearer"
        ):
            return "", _branch("invalidResponse", FailureKind.PROTOCOL, "Honor returned invalid token data")
        return document["access_token"], None

    def _decode_app(self, body: bytes, package_name: str) -> QueryAttempt[LookupAppIDOutput]:
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            document = None
        if not isinstance(document, dict) or type(document.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Honor returned invalid app data")
        if document["code"] != 0:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Honor rejected app request with code {document['code']}",
            )
        data = document.get("data")
        if not isinstance(data, list):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Honor returned invalid app data")
        if not data:
            return QueryAttempt.branch_result(
                "notFound", LookupAppIDOutput(package_name=package_name), Receipt()
            )
        if (
            len(data) != 1
            or not isinstance(data[0], dict)
            or data[0].get("packageName") != package_name
            or type(data[0].get("appId")) is not int
            or data[0]["appId"] <= 0
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Honor returned mismatched app data")
        return QueryAttempt.branch_result(
            "found", LookupAppIDOutput(package_name=package_name, app_id=data[0]["appId"]), Receipt()
        )
