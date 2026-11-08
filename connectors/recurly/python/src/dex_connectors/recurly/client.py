# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Recurly account reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNT_DEFINITION
from .models import AccountIdentity, GetAccountInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "recurly", "getAccount", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccountIdentity]:
    return QueryAttempt.branch_result(branch, AccountIdentity(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccountIdentity]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_account_id(account_id: object) -> bool:
    return (
        isinstance(account_id, str)
        and 0 < len(account_id) <= 55
        and account_id != "code-"
        and all(
            character in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
            for character in account_id
        )
    )


def _decode_account(body: bytes, requested_account_id: str) -> AccountIdentity:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("account object missing")
    account_id = raw.get("id")
    code = raw.get("code")
    if (
        not isinstance(account_id, str)
        or not _valid_account_id(account_id)
        or not isinstance(code, str)
        or not code
        or (requested_account_id.startswith("code-") and code != requested_account_id[5:])
        or (not requested_account_id.startswith("code-") and account_id != requested_account_id)
    ):
        raise ValueError("account identity does not match request")
    return AccountIdentity(account_id, code)


class Client:
    """Resolve an API key per call and read selected account metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Recurly configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Recurly request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_account(self) -> GetAccountOperation:
        """Bind the getAccount Query to this client."""
        return GetAccountOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, account_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_key):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/accounts/" + quote(account_id, safe=""),
                headers={
                    "Accept": "application/vnd.recurly.v2021-02-25+json",
                    "Authorization": "Basic " + base64.b64encode((api_key + ":").encode()).decode(),
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetAccountOperation:
    """Read one account's ID, name, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_ACCOUNT_DEFINITION

    async def invoke(self, call: Call, input: GetAccountInput) -> QueryAttempt[AccountIdentity]:
        """Classify account lookup outcomes consistently with Go."""
        if not isinstance(input, GetAccountInput) or not _valid_account_id(input.account_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "account ID must be 1–55 URL-safe characters",
            )
        exchange = await self.client._get(call, input.account_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Recurly credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Recurly request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Recurly rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Recurly is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Recurly did not expose the account")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Recurly rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Recurly response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Recurly response was interrupted")
        try:
            account_identity = _decode_account(exchange.body, input.account_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Recurly returned invalid account JSON")
        return QueryAttempt.branch_result("found", account_identity, Receipt())
