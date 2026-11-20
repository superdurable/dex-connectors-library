# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Qiyuesuo signed contract-list transport and Go-compatible decisions."""

import hashlib
import json
import time
import uuid
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
from ._generated.definitions import LIST_CONTRACTS_DEFINITION
from .models import ContractSummary, ListContractsInput, ListContractsOutput

_STATUSES = {
    "DRAFT",
    "RECALLED",
    "SIGNING",
    "REJECTED",
    "COMPLETE",
    "EXPIRED",
    "FILLING",
    "INVALIDING",
    "INVALIDED",
    "FORCE_END",
}
_AUTHENTICATION_CODES = {"11990009", "11990440", "11990441", "11990442", "11990446", "11990448"}
_AUTHORIZATION_CODES = {"14990002", "14990021", "11990403", "11990449"}


def _credential(value: str) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 512 and all(0x21 <= ord(ch) <= 0x7E for ch in value)


class Client:
    """Resolve contract API credentials for each Query and own the HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock_milliseconds: Callable[[], int] | None = None,
        nonce_factory: Callable[[], str] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Qiyuesuo configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or parsed.netloc not in ("openapi.qiyuesuo.com", "openapi.qiyuesuo.cn")
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Qiyuesuo base URL must be an official HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock_milliseconds = clock_milliseconds or (lambda: int(time.time() * 1000))
        self.nonce_factory = nonce_factory or (lambda: str(uuid.uuid4()))

    def list_contracts(self) -> "ListContractsOperation":
        """Bind the listContracts Query."""
        return ListContractsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListContractsOperation:
    """Read one bounded contract-status page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_CONTRACTS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListContractsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListContractsOutput(),
            Receipt(),
            Failure(kind, "qiyuesuo", "listContracts", "Qiyuesuo contract query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListContractsOutput]:
        return QueryAttempt.retry(
            Failure(kind, "qiyuesuo", "listContracts", "Qiyuesuo request failed"), delay
        )

    def _provider_code(self, code: str) -> QueryAttempt[ListContractsOutput]:
        if code == "11990006":
            return self._retry(FailureKind.RATE_LIMIT)
        if code == "21990001":
            return self._retry(FailureKind.AVAILABILITY)
        kind = (
            FailureKind.AUTHENTICATION
            if code in _AUTHENTICATION_CODES
            else FailureKind.AUTHORIZATION
            if code in _AUTHORIZATION_CODES
            else FailureKind.PROVIDER_REJECTION
        )
        return self._branch("providerRejected", kind)

    async def invoke(self, call: Call, input: ListContractsInput) -> QueryAttempt[ListContractsOutput]:
        """List contract IDs and status while keeping signing secrets out of results."""
        if not isinstance(input, ListContractsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token, secret = credentials.app_token.reveal(), credentials.app_secret.reveal()
        if not _credential(token) or not _credential(secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        limit = input.limit or 20
        if input.offset < 0 or input.offset > 1000000 or limit < 1 or limit > 100:
            return self._branch("defect", FailureKind.VALIDATION)
        timestamp, nonce = str(self.client.clock_milliseconds()), self.client.nonce_factory()
        signature = hashlib.md5((token + secret + timestamp + nonce).encode()).hexdigest()
        request = self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + "/v2/contract/list",
            params=[("selectLimit", str(limit)), ("selectOffset", str(input.offset))],
            headers={
                "Accept": "application/json",
                "x-qys-open-accesstoken": token,
                "x-qys-open-timestamp": timestamp,
                "x-qys-open-nonce": nonce,
                "x-qys-open-signature": signature,
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
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status in (401, 440, 441, 442, 446)
                    else FailureKind.AUTHORIZATION
                    if status in (403, 449)
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("responseCode"), str)
            or not wire["responseCode"]
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["responseCode"] != "00000000":
            return self._provider_code(wire["responseCode"])
        result = wire.get("result")
        if not isinstance(result, dict):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        total, items = result.get("totalCount"), result.get("list")
        if not isinstance(total, int) or isinstance(total, bool) or total < 0 or not isinstance(items, list):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if len(items) > limit or total < input.offset + len(items):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        contracts: list[ContractSummary] = []
        for item in items:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            identifier, state = item.get("id"), item.get("status")
            if (
                not isinstance(identifier, int)
                or isinstance(identifier, bool)
                or identifier <= 0
                or identifier > 9223372036854775807
                or state not in _STATUSES
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            contracts.append(ContractSummary(id=str(identifier), status=state))
        next_offset = 0
        if input.offset + len(contracts) < total:
            if not contracts or input.offset + len(contracts) > 1000000:
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            next_offset = input.offset + len(contracts)
        return QueryAttempt.branch_result(
            "listed", ListContractsOutput(tuple(contracts), total, next_offset), Receipt()
        )
