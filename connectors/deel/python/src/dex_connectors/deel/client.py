# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Deel contract reads with Go-compatible outcomes."""

from __future__ import annotations

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
from ._generated.definitions import GET_CONTRACT_DEFINITION
from .models import ContractStatus, GetContractInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "deel", "getContract", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ContractStatus]:
    return QueryAttempt.branch_result(branch, ContractStatus(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ContractStatus]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_contract_id(contract_id: object) -> bool:
    return (
        isinstance(contract_id, str)
        and 1 <= len(contract_id) <= 128
        and all(
            character in "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789-_"
            for character in contract_id
        )
    )


def _decode_contract(body: bytes, requested_contract_id: str) -> ContractStatus:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("data"), dict):
        raise ValueError("contract data missing")
    data = raw["data"]
    if (
        data.get("id") != requested_contract_id
        or not isinstance(data.get("status"), str)
        or not data["status"].strip()
        or not isinstance(data.get("type"), str)
        or not data["type"].strip()
    ):
        raise ValueError("contract identity, type, or status missing")
    return ContractStatus(data["id"], data["type"], data["status"])


class Client:
    """Resolve an API token per call and read selected contract metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Deel configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Deel request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_contract(self) -> GetContractOperation:
        """Bind the getContract Query to this client."""
        return GetContractOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, contract_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            api_token = credential.api_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(api_token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/contracts/" + quote(contract_id, safe=""),
                headers={"Accept": "application/json", "Authorization": "Bearer " + api_token},
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


class GetContractOperation:
    """Read one contract's ID, type, and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_CONTRACT_DEFINITION

    async def invoke(self, call: Call, input: GetContractInput) -> QueryAttempt[ContractStatus]:
        """Classify contract lookup outcomes consistently with Go."""
        if not isinstance(input, GetContractInput) or not _valid_contract_id(input.contract_id):
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "contract ID must be 1–128 URL-safe characters",
            )
        exchange = await self.client._get(call, input.contract_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Deel credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Deel request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Deel rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Deel is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Deel did not expose the contract")
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Deel rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Deel response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Deel response was interrupted")
        try:
            contract_status = _decode_contract(exchange.body, input.contract_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Deel returned invalid contract JSON"
            )
        return QueryAttempt.branch_result("found", contract_status, Receipt())
