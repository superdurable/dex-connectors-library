# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Braintree GraphQL transaction lookup with Go-compatible outcomes."""

from __future__ import annotations

import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, date, datetime
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
from ._generated.definitions import GET_TRANSACTION_DEFINITION
from .models import GetTransactionInput, TransactionProfile

_GET_TRANSACTION_GRAPHQL = (
    "query GetTransaction($id: ID!) { node(id: $id) { __typename ... on Transaction { id status } } }"
)
_API_VERSION = re.compile(r"[0-9]{4}-[0-9]{2}-[0-9]{2}\Z")


class _GraphQLRejection(Exception):
    """Braintree rejected the GraphQL query."""


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "braintree", "getTransaction", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TransactionProfile]:
    return QueryAttempt.branch_result(branch, TransactionProfile(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TransactionProfile]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_graphql_id(value: object) -> bool:
    if not isinstance(value, str) or not value or value.strip() != value:
        return False
    try:
        return len(value.encode("utf-8")) <= 128 and all(
            ord(char) >= 0x20 and ord(char) != 0x7F for char in value
        )
    except UnicodeEncodeError:
        return False


def _decode_transaction(body: bytes, transaction_id: str) -> TransactionProfile | None:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict):
        raise ValueError("GraphQL response is not an object")
    errors = raw.get("errors", [])
    if errors is not None and not isinstance(errors, list):
        raise ValueError("GraphQL errors are invalid")
    if isinstance(errors, list) and errors:
        raise _GraphQLRejection()
    data = raw.get("data")
    if not isinstance(data, dict):
        raise ValueError("GraphQL data is missing")
    node = data.get("node")
    if node is None:
        return None
    if not isinstance(node, dict):
        raise ValueError("GraphQL node is invalid")
    if not isinstance(node.get("__typename"), str) or not node["__typename"]:
        raise ValueError("GraphQL node type is missing")
    if node["__typename"] != "Transaction":
        return None
    if node.get("id") != transaction_id or not isinstance(node.get("status"), str) or not node["status"]:
        raise ValueError("transaction identity or status is invalid")
    return TransactionProfile(node["id"], node["status"])


class Client:
    """Resolve Braintree API keys per call and read transactions."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Braintree configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Braintree request limits must be positive")
        if not _API_VERSION.fullmatch(config.api_version):
            raise ValueError("Braintree API version must be YYYY-MM-DD")
        try:
            date.fromisoformat(config.api_version)
        except ValueError as error:
            raise ValueError("Braintree API version must be a valid date") from error
        self.api_version = config.api_version
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_transaction(self) -> GetTransactionOperation:
        """Bind the getTransaction Query to this client."""
        return GetTransactionOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, input: GetTransactionInput) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            public_key = credential.public_key
            private_key = credential.private_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if (
            not is_header_safe_credential(public_key)
            or ":" in public_key
            or not is_header_safe_credential(private_key)
        ):
            return _Exchange(error="credential")
        basic = base64.b64encode((public_key + ":" + private_key).encode("utf-8")).decode("ascii")
        try:
            request = self.http_client.build_request(
                "POST",
                self.base_url + "/graphql",
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "Braintree-Version": self.api_version,
                    "Authorization": "Basic " + basic,
                },
                json={"query": _GET_TRANSACTION_GRAPHQL, "variables": {"id": input.transaction_id}},
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


class GetTransactionOperation:
    """Read one transaction by its Braintree GraphQL ID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return GET_TRANSACTION_DEFINITION

    async def invoke(self, call: Call, input: GetTransactionInput) -> QueryAttempt[TransactionProfile]:
        """Classify provider outcomes consistently with Go."""
        if not isinstance(input, GetTransactionInput) or not _valid_graphql_id(input.transaction_id):
            return _branch(
                "defect", FailureKind.VALIDATION, "transactionId must contain 1–128 printable characters"
            )
        exchange = await self.client._get(call, input)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Braintree credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Braintree request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Braintree rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Braintree is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Braintree rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Braintree response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Braintree response was interrupted")
        try:
            profile = _decode_transaction(exchange.body, input.transaction_id)
        except _GraphQLRejection:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, "Braintree rejected the GraphQL query"
            )
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Braintree returned invalid transaction JSON"
            )
        if profile is None:
            return _branch("notFound", FailureKind.NOT_FOUND, "Braintree did not expose the transaction")
        return QueryAttempt.branch_result("found", profile, Receipt())
