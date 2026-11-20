# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Qince signed department-query transport and Go-compatible decisions."""

import hashlib
import json
import secrets
import time
from collections.abc import Callable
from datetime import UTC, datetime
from typing import cast
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
from ._generated.definitions import LIST_DEPARTMENTS_DEFINITION
from .models import Department, ListDepartmentsInput, ListDepartmentsOutput


def _alphanumeric(value: str, length: int) -> bool:
    return isinstance(value, str) and len(value) == length and value.isascii() and value.isalnum()


def _safe_metadata(value: object, maximum: int) -> bool:
    return (
        isinstance(value, str)
        and bool(value)
        and len(value) <= maximum
        and value.strip() == value
        and not any(character in value for character in "\r\n\0")
    )


class Client:
    """Resolve rotated Qince credentials for each Query and own the HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock_milliseconds: Callable[[], int] | None = None,
        message_id_factory: Callable[[], str] | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Qince configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        host = parsed.hostname or ""
        if (
            parsed.scheme != "https"
            or (
                host != "openapi.qince.com"
                and not (host.startswith("openapi.") and host.endswith(".qince.com"))
            )
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.username
            or parsed.password
        ):
            raise ValueError("Qince base URL must be an official HTTPS OpenAPI origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.clock_milliseconds = clock_milliseconds or (lambda: int(time.time() * 1000))
        self.message_id_factory = message_id_factory or (lambda: secrets.token_hex(16))

    def list_departments(self) -> "ListDepartmentsOperation":
        """Bind the listDepartments Query."""
        return ListDepartmentsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListDepartmentsOperation:
    """Read bounded department metadata through Qince's signed OpenAPI."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_DEPARTMENTS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListDepartmentsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListDepartmentsOutput(),
            Receipt(),
            Failure(kind, "qince", "listDepartments", "Qince department query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListDepartmentsOutput]:
        return QueryAttempt.retry(
            Failure(kind, "qince", "listDepartments", "Qince request failed"),
            delay,
        )

    async def invoke(self, call: Call, input: ListDepartmentsInput) -> QueryAttempt[ListDepartmentsOutput]:
        """Run one signed request; keep the AppKey out of Dex state and results."""
        if not isinstance(input, ListDepartmentsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _alphanumeric(credentials.open_id, 19) or not _alphanumeric(credentials.app_key.reveal(), 18):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        timestamp = str(self.client.clock_milliseconds())
        message_id = self.client.message_id_factory()
        secret = credentials.app_key.reveal()
        digest = hashlib.md5(("{}|" + secret + "|" + timestamp).encode()).hexdigest()
        endpoint = (
            self.client.config.base_url.rstrip("/")
            + "/api/organization/v1/queryOrganization/"
            + credentials.open_id
            + "/"
            + timestamp
            + "/"
            + digest
            + "/"
            + message_id
        )
        request = self.client.http_client.build_request(
            "POST",
            endpoint,
            content=b"{}",
            headers={"Accept": "application/json", "Content-Type": "application/json"},
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
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or wire.get("msg_id") != message_id or "return_code" not in wire:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        code = wire["return_code"]
        if not isinstance(code, (str, int)) or isinstance(code, bool):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if str(code) != "0":
            return self._branch("providerRejected", FailureKind.PROVIDER_REJECTION)
        records = wire.get("response_data")
        if isinstance(records, str):
            try:
                records = json.loads(records)
            except ValueError:
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(records, list) or len(records) > 10000:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        departments: list[Department] = []
        for record in records:
            if not isinstance(record, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            department_id, code, name, active = (
                record.get("id"),
                record.get("org_code"),
                record.get("org_name"),
                record.get("org_status"),
            )
            if not all(
                (_safe_metadata(department_id, 32), _safe_metadata(code, 100), _safe_metadata(name, 100))
            ) or active not in ("0", "1"):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            departments.append(
                Department(
                    id=cast(str, department_id),
                    code=cast(str, code),
                    name=cast(str, name),
                    active=active == "1",
                )
            )
        return QueryAttempt.branch_result("listed", ListDepartmentsOutput(tuple(departments)), Receipt())
