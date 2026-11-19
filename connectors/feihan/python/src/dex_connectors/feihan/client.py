# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Feihan order status Query with Go-compatible signing and decisions."""

import hashlib
import json
from datetime import UTC, datetime
from urllib.parse import urlencode, urlsplit

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
from ._generated.definitions import QUERY_RECHARGE_ORDER_DEFINITION
from .models import QueryRechargeOrderInput, RechargeOrderResult


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[RechargeOrderResult]:
    return QueryAttempt.branch_result(
        branch, RechargeOrderResult(), Receipt(), Failure(kind, "feihan", "queryRechargeOrder", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[RechargeOrderResult]:
    return QueryAttempt.retry(Failure(kind, "feihan", "queryRechargeOrder", message), delay)


def _valid_order_id(value: str) -> bool:
    return bool(value) and len(value) <= 64 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _valid_project_id(value: str) -> bool:
    return (
        2 <= len(value) <= 32
        and value.startswith("A")
        and all(c in "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789" for c in value[1:])
    )


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 2048 and value.strip() == value


class Client:
    """Own Feihan settings and resolve credentials for each order query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Feihan configuration and credential source are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or not parsed.hostname
            or parsed.username is not None
            or parsed.password is not None
            or parsed.query
            or parsed.fragment
            or parsed.path not in ("", "/")
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Feihan configuration is invalid")
        self.base_url = config.base_url.rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def query_recharge_order(self) -> "QueryRechargeOrderOperation":
        """Bind the recharge order Query."""
        return QueryRechargeOrderOperation(self)

    async def aclose(self) -> None:
        """Close an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class QueryRechargeOrderOperation:
    """Read one existing Feihan order status."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Feihan client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry policy."""
        return QUERY_RECHARGE_ORDER_DEFINITION

    async def invoke(self, call: Call, input: QueryRechargeOrderInput) -> QueryAttempt[RechargeOrderResult]:
        """Sign and send one query, then classify the response."""
        order_id = input.merchant_order_id if isinstance(input, QueryRechargeOrderInput) else ""
        if not _valid_order_id(order_id):
            return _branch("defect", FailureKind.VALIDATION, "Feihan merchant order ID is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            project_id = credentials.project_id
            password = credentials.password.reveal()
            key_str = credentials.key_str.reveal()
            if (
                not _valid_project_id(project_id)
                or not _valid_credential(password)
                or not _valid_credential(key_str)
            ):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Feihan credentials are unavailable or invalid"
            )
        password_hash = hashlib.md5(password.encode()).hexdigest()  # noqa: S324 - provider protocol
        signature = (
            hashlib.md5(  # noqa: S324 - provider protocol
                f"{project_id}{password_hash}{order_id}{key_str}".encode()
            )
            .hexdigest()
            .upper()
        )
        form = urlencode(
            {
                "userid": project_id,
                "userpws": password_hash,
                "sporder_id": order_id,
                "md5_str": signature,
                "version": "6.0",
                "format": "json",
            }
        )
        try:
            request = self.client.http_client.build_request(
                "POST",
                f"{self.client.base_url}/queryOrderInfo.do",
                headers={"Content-Type": "application/x-www-form-urlencoded", "Accept": "application/json"},
                content=form.encode(),
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "Feihan order request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Feihan order request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Feihan rate limited order query", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Feihan order API is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Feihan response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Feihan order response was interrupted")
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else (
                        FailureKind.AUTHORIZATION
                        if status == 403
                        else (
                            FailureKind.VALIDATION
                            if status in (400, 404, 412)
                            else FailureKind.PROVIDER_REJECTION
                        )
                    )
                )
                return _branch("providerRejected", kind, f"Feihan rejected order query with HTTP {status}")
        finally:
            await response.aclose()
        return _decode_order(body, order_id)


def _decode_order(body: bytes, order_id: str) -> QueryAttempt[RechargeOrderResult]:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Feihan returned an invalid order response")
    if (
        not isinstance(document, dict)
        or not isinstance(document.get("retcode"), str)
        or not document["retcode"]
    ):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Feihan returned an invalid order response")
    if document["retcode"] != "1":
        return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "Feihan rejected order query")
    status = document.get("game_state")
    if document.get("sporder_id") != order_id or type(status) is not int or status not in (-1, 0, 1, 9):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Feihan returned an invalid order response")
    if status == -1:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            "Feihan could not find the order; manual verification is required",
        )
    return QueryAttempt.branch_result("found", RechargeOrderResult(order_id, status), Receipt())
