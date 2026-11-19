# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""taosdata provider transport and Go-compatible Query decisions."""

import json
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_DATABASES_DEFINITION
from ._generated.models import DatabaseMetadata
from .models import ListDatabasesInput, ListDatabasesOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListDatabasesOutput]:
    return QueryAttempt.branch_result(
        branch, ListDatabasesOutput(), Receipt(), Failure(kind, "taosdata", "listDatabases", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListDatabasesOutput]:
    return QueryAttempt.retry(Failure(kind, "taosdata", "listDatabases", message), delay)


class Client:
    """Resolve credentials per listDatabases request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("taosdata configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("taosdata timeout or response limit is invalid")
        self.config = config
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_databases(self) -> "ListDatabasesOperation":
        """Bind the listDatabases Query to this client."""
        return ListDatabasesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListDatabasesOperation:
    """Perform the listDatabases read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("taosdata client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return LIST_DATABASES_DEFINITION

    async def invoke(self, call: Call, input: ListDatabasesInput) -> QueryAttempt[ListDatabasesOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._build_request(input, credentials)
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "taosdata request failed")
        return await self._process_response(response, input, credentials)

    async def _process_response(
        self, response: httpx.Response, input: ListDatabasesInput, credentials: Credentials
    ) -> QueryAttempt[ListDatabasesOutput]:
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.AVAILABILITY, "taosdata rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "taosdata is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "taosdata response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "taosdata response was interrupted")
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, f"taosdata rejected request with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "taosdata returned invalid JSON")
        return self._decode(input, document)

    def _build_request(self, input: ListDatabasesInput, credentials: Credentials) -> httpx.Request:
        cloud_url = validate_base_url(credentials.cloud_url.reveal())
        token = credentials.cloud_token.reveal()
        if not token or any(ord(ch) <= 32 for ch in token):
            raise ValueError("invalid token")
        return self.client.http_client.build_request(
            "POST",
            cloud_url + "/rest/sql",
            params={"token": token},
            content="select name, ntables, status from information_schema.ins_databases;",
            headers={"Content-Type": "text/plain"},
        )

    def _decode(self, input: ListDatabasesInput, document: Any) -> QueryAttempt[ListDatabasesOutput]:
        if not isinstance(document, dict) or type(document.get("code")) is not int:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid metadata response")
        if document["code"] != 0:
            return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "metadata query rejected")
        columns = document.get("column_meta")
        rows = document.get("data")
        if (
            not isinstance(columns, list)
            or len(columns) != 3
            or [col[0] if isinstance(col, list) and col else None for col in columns]
            != ["name", "ntables", "status"]
            or not isinstance(rows, list)
            or type(document.get("rows")) is not int
            or document["rows"] != len(rows)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid metadata columns")
        databases = []
        for row in rows:
            if (
                not isinstance(row, list)
                or len(row) != 3
                or not isinstance(row[0], str)
                or not row[0]
                or type(row[1]) is not int
                or row[1] < 0
                or not isinstance(row[2], str)
                or not row[2]
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid metadata row")
            databases.append(DatabaseMetadata(name=row[0], table_count=row[1], status=row[2]))
        return QueryAttempt.branch_result(
            "listed", ListDatabasesOutput(databases=tuple(databases)), Receipt()
        )
