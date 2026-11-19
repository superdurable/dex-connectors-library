# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sign Kingsoft Cloud IAM requests and read one bounded user page."""

import hashlib
import hmac
import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote

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
from ._generated.definitions import LIST_USERS_DEFINITION
from ._generated.models import IAMUser
from .models import ListUsersInput, ListUsersOutput


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "kingsoftcloud", "listUsers", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListUsersOutput]:
    return QueryAttempt.branch_result(branch, ListUsersOutput(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListUsersOutput]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_credential(value: str) -> bool:
    return (
        bool(value) and len(value) <= 512 and value.strip() == value and all(32 < ord(c) < 127 for c in value)
    )


def _canonical_query(parameters: dict[str, str]) -> str:
    return "&".join(
        quote(key, safe="~") + "=" + quote(parameters[key], safe="~") for key in sorted(parameters)
    )


class Client:
    """Resolve current IAM keys and sign each read with an injectable clock."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
            or not config.region
            or not config.service
        ):
            raise ValueError("Kingsoft Cloud configuration and credentials are required")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.region = config.region
        self.service = config.service
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def list_users(self) -> "ListUsersOperation":
        """Bind the one-page IAM user Query."""
        return ListUsersOperation(self)

    async def aclose(self) -> None:
        """Close a connector-owned transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListUsersOperation:
    """Read IAM user IDs and names without contact metadata."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_USERS_DEFINITION

    async def invoke(self, call: Call, input: ListUsersInput) -> QueryAttempt[ListUsersOutput]:
        """Sign and classify one ListUsers provider request."""
        if (
            not isinstance(input, ListUsersInput)
            or input.max_items < 0
            or input.max_items > 100
            or len(input.marker) > 320
            or input.marker.strip() != input.marker
        ):
            return _branch("defect", FailureKind.VALIDATION, "IAM page size or marker is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key = credentials.access_key_id.reveal()
            secret_key = credentials.secret_access_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch("defect", FailureKind.AUTHENTICATION, "IAM credentials are unavailable")
        if not _valid_credential(access_key) or not _valid_credential(secret_key):
            return _branch("defect", FailureKind.AUTHENTICATION, "IAM credentials are invalid")
        timestamp = self.client.now().astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
        parameters = {
            "Accesskey": access_key,
            "Action": "ListUsers",
            "Service": self.client.service,
            "Region": self.client.region,
            "Version": "2015-11-01",
            "Timestamp": timestamp,
            "SignatureVersion": "1.0",
            "SignatureMethod": "HMAC-SHA256",
            "MaxItems": str(input.max_items or 100),
        }
        if input.marker:
            parameters["Marker"] = input.marker
        signature = hmac.new(
            secret_key.encode(), _canonical_query(parameters).encode(), hashlib.sha256
        ).hexdigest()
        parameters["Signature"] = signature
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/?" + _canonical_query(parameters),
                headers={"Accept": "application/json"},
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "IAM request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "IAM rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "IAM is temporarily unavailable", delay)
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"IAM rejected HTTP {status}"
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "IAM response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "IAM response was interrupted")
            try:
                wire: Any = json.loads(body)
                result = wire["ListUserResult"]
                members = result["Users"]["member"]
                if (
                    not isinstance(members, list)
                    or len(members) > 100
                    or type(result.get("IsTruncated", False)) is not bool
                    or (result.get("IsTruncated", False) and not result.get("Marker"))
                ):
                    raise ValueError("invalid IAM user page")
                users = tuple(
                    IAMUser(user_id=member["UserId"], user_name=member["UserName"]) for member in members
                )
                if any(not user.user_id or not user.user_name for user in users):
                    raise ValueError("invalid IAM user")
                has_more = result.get("IsTruncated", False)
                next_marker = result.get("Marker", "") if has_more else ""
            except (KeyError, IndexError, TypeError, ValueError, UnicodeDecodeError):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "IAM returned an invalid response")
            return QueryAttempt.branch_result(
                "listed", ListUsersOutput(users, has_more, next_marker), Receipt()
            )
        finally:
            await response.aclose()
