# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Plaid accounts reads with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_ACCOUNTS_DEFINITION
from .models import AccountsSnapshot, AccountSummary, GetAccountsInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "plaid", "getAccounts", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[AccountsSnapshot]:
    return QueryAttempt.branch_result(branch, AccountsSnapshot(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[AccountsSnapshot]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_item_id(item_id: object) -> bool:
    return (
        isinstance(item_id, str)
        and 1 <= len(item_id) <= 128
        and all(
            character in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
            for character in item_id
        )
    )


def _is_item_not_found(body: bytes) -> bool:
    """Report Plaid's ITEM_NOT_FOUND error, sent with HTTP 400 for a removed or unknown Item."""
    try:
        raw: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    return (
        isinstance(raw, dict)
        and raw.get("error_type") == "ITEM_ERROR"
        and raw.get("error_code") == "ITEM_NOT_FOUND"
    )


def _decode_accounts(body: bytes, requested_item_id: str) -> AccountsSnapshot:
    raw: Any = json.loads(body)
    if (
        not isinstance(raw, dict)
        or not isinstance(raw.get("item"), dict)
        or raw["item"].get("item_id") != requested_item_id
        or not isinstance(raw.get("accounts"), list)
    ):
        raise ValueError("Item identity or accounts missing")
    accounts: list[AccountSummary] = []
    for account in raw["accounts"]:
        if (
            not isinstance(account, dict)
            or not isinstance(account.get("account_id"), str)
            or not account["account_id"].strip()
            or not isinstance(account.get("type"), str)
            or not account["type"].strip()
        ):
            raise ValueError("account identity or type missing")
        subtype = account.get("subtype")
        if subtype is not None and not isinstance(subtype, str):
            raise ValueError("account subtype is invalid")
        accounts.append(AccountSummary(account["account_id"], account["type"], subtype or ""))
    return AccountsSnapshot(requested_item_id, tuple(accounts))


class Client:
    """Resolve Item credentials per call and read account routing fields."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Plaid configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Plaid request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_accounts(self) -> GetAccountsOperation:
        """Bind the getAccounts Query to this client."""
        return GetAccountsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get_accounts(self, call: Call) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            client_id = credential.client_id.reveal()
            secret = credential.secret.reveal()
            access_token = credential.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if (
            not is_header_safe_credential(client_id)
            or not is_header_safe_credential(secret)
            or not access_token
        ):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/accounts/get",
                headers={"Accept": "application/json", "PLAID-CLIENT-ID": client_id, "PLAID-SECRET": secret},
                json={"access_token": access_token},
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


class GetAccountsOperation:
    """Read account IDs, types, and subtypes for one linked Item."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated account Query."""
        return GET_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: GetAccountsInput) -> QueryAttempt[AccountsSnapshot]:
        """Classify Item account lookup outcomes consistently with Go."""
        if not isinstance(input, GetAccountsInput) or not _valid_item_id(input.item_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "Item ID must be 1–128 URL-safe characters",
            )
        exchange = await self.client._get_accounts(call)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Plaid credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Plaid request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Plaid rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Plaid is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404 or (not exchange.error and _is_item_not_found(exchange.body)):
            return _branch("notFound", FailureKind.NOT_FOUND, "Plaid did not expose the Item")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Plaid rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Plaid response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Plaid response was interrupted")
        try:
            accounts_status = _decode_accounts(exchange.body, input.item_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Plaid returned invalid accounts JSON")
        return QueryAttempt.branch_result("found", accounts_status, Receipt())
