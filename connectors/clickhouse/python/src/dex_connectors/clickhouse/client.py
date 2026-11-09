# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded ClickHouse table listing with Go-compatible outcomes."""

from __future__ import annotations

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlencode

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
from ._generated.definitions import LIST_TABLES_DEFINITION
from .models import ListTablesInput, TablesPage

_LIST_TABLES_SQL = (
    "SELECT name FROM system.tables WHERE database = {database:String} ORDER BY name LIMIT 101 FORMAT JSON"
)


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or request failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "clickhouse", "listTables", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TablesPage]:
    return QueryAttempt.branch_result(branch, TablesPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TablesPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


def _valid_database(database: object) -> bool:
    if not isinstance(database, str) or not database or database.strip() != database:
        return False
    try:
        byte_count = len(database.encode("utf-8"))
    except UnicodeEncodeError:
        return False
    return byte_count <= 128 and all(
        ord(character) >= 0x20 and ord(character) != 0x7F for character in database
    )


def _decode_tables(body: bytes, database: str) -> TablesPage:
    raw: Any = json.loads(body)
    if not isinstance(raw, dict) or not isinstance(raw.get("data"), list):
        raise ValueError("table page is missing")
    data = raw["data"]
    if len(data) > 101 or type(raw.get("rows")) is not int or raw["rows"] != len(data):
        raise ValueError("table page is inconsistent")
    names: list[str] = []
    for row in data:
        if not isinstance(row, dict) or not isinstance(row.get("name"), str) or not row["name"]:
            raise ValueError("table name is missing")
        if len(names) < 100:
            names.append(row["name"])
    return TablesPage(database, tuple(names), len(data) > 100)


class Client:
    """Resolve database credentials per call and list table names."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("ClickHouse configuration and credential source are required")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("ClickHouse request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_tables(self) -> ListTablesOperation:
        """Bind the listTables Query to this client."""
        return ListTablesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get(self, call: Call, database: str) -> _Exchange:
        try:
            credential = await self.credentials.resolve(call)
            username = credential.username
            password = credential.password.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not is_header_safe_credential(username) or not is_header_safe_credential(password):
            return _Exchange(error="credential")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + "/?" + urlencode({"query": _LIST_TABLES_SQL, "param_database": database}),
                headers={
                    "Accept": "application/json",
                    "X-ClickHouse-User": username,
                    "X-ClickHouse-Key": password,
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


class ListTablesOperation:
    """List up to 100 sorted table names in one database."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query worker."""
        return LIST_TABLES_DEFINITION

    async def invoke(self, call: Call, input: ListTablesInput) -> QueryAttempt[TablesPage]:
        """Classify table listing outcomes consistently with Go."""
        if not isinstance(input, ListTablesInput) or not _valid_database(input.database):
            return _branch(
                "defect", FailureKind.VALIDATION, "database must contain 1–128 printable characters"
            )
        exchange = await self.client._get(call, input.database)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "ClickHouse credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "ClickHouse request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "ClickHouse rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "ClickHouse is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"ClickHouse rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "ClickHouse response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "ClickHouse response was interrupted")
        try:
            page = _decode_tables(exchange.body, input.database)
        except (ValueError, TypeError, UnicodeDecodeError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "ClickHouse returned invalid table-list JSON"
            )
        return QueryAttempt.branch_result("listed", page, Receipt())
