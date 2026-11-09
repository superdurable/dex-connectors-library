# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Miro board reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_BOARD_DEFINITION
from .models import BoardMetadata, GetBoardInput


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "miro", "getBoard", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[BoardMetadata]:
    return QueryAttempt.branch_result(branch, BoardMetadata(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[BoardMetadata]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_board_id(board_id: object) -> bool:
    return (
        isinstance(board_id, str)
        and bool(board_id)
        and len(board_id.encode("utf-8")) <= 256
        and not any(ord(char) < 32 or 127 <= ord(char) <= 159 or char in "/?#" for char in board_id)
    )


def _decode_board(body: bytes, requested_board_id: str) -> BoardMetadata:
    raw: Any = json.loads(body)
    if (
        not isinstance(raw, dict)
        or raw.get("id") != requested_board_id
        or not isinstance(raw.get("name"), str)
        or not raw["name"].strip()
        or raw.get("type") != "board"
    ):
        raise ValueError("board identity or name missing")
    return BoardMetadata(raw["id"], raw["name"])


class Client:
    """Resolve a Bearer token per call and read selected board metadata."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Miro configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Miro request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_board(self) -> GetBoardOperation:
        """Bind the getBoard Query to this client."""
        return GetBoardOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, board_id: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            token = credential.access_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(token):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/boards/" + quote(board_id, safe=""),
                headers={"Accept": "application/json", "Authorization": "Bearer " + token},
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


class GetBoardOperation:
    """Read one board's ID and name."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_BOARD_DEFINITION

    async def invoke(self, call: Call, input: GetBoardInput) -> QueryAttempt[BoardMetadata]:
        """Classify board lookup outcomes consistently with Go."""
        if not isinstance(input, GetBoardInput) or not _valid_board_id(input.board_id):
            return _branch("defect", FailureKind.VALIDATION, "board ID must be 1–256 printable characters")
        exchange = await self.client._get(call, input.board_id)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Miro credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Miro request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Miro rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Miro is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return _branch("notFound", FailureKind.NOT_FOUND, "Miro did not expose the board")
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Miro rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Miro response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Miro response was interrupted")
        try:
            board = _decode_board(exchange.body, input.board_id)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Miro returned invalid board JSON")
        return QueryAttempt.branch_result("found", board, Receipt())
