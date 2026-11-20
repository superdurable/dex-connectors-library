# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Weiling department Query and short-lived access-token transport."""

import asyncio
import hashlib
import json
import time
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
from ._generated.definitions import LIST_DEPARTMENTS_DEFINITION
from .models import DepartmentSummary, ListDepartmentsInput, ListDepartmentsOutput


def _valid_credential(value: str) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= 2048 and all(0x21 <= ord(char) <= 0x7E for char in value)
    )


class Client:
    """Own configuration, credential source, HTTP client, and a token cache per credential pair."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("Weiling configuration and credentials are required")
        parsed = urlsplit(config.base_url)
        if (
            parsed.scheme != "https"
            or parsed.hostname != "openapi.weiling.cn"
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.username
            or parsed.password
        ):
            raise ValueError("Weiling base URL must be the documented HTTPS origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self._token_lock = asyncio.Lock()
        self._cached_token = ""
        self._credential_digest = b""
        self._token_expires_at = 0.0

    def list_departments(self) -> "ListDepartmentsOperation":
        """Bind the listDepartments Query to this client."""
        return ListDepartmentsOperation(self)

    async def aclose(self) -> None:
        """Close the transport only when this client created it."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListDepartmentsOperation:
    """Read the accessible department directory without employee identifiers."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query definition."""
        return LIST_DEPARTMENTS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListDepartmentsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListDepartmentsOutput(),
            Receipt(),
            Failure(kind, "weiling", "listDepartments", "Weiling department query failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListDepartmentsOutput]:
        return QueryAttempt.retry(
            Failure(kind, "weiling", "listDepartments", "Weiling request failed"), delay
        )

    async def _response(
        self, call: Call, path: str, parameters: dict[str, str]
    ) -> bytes | QueryAttempt[ListDepartmentsOutput]:
        request = self.client.http_client.build_request(
            "GET",
            self.client.config.base_url.rstrip("/") + path,
            params=parameters,
            headers={"Accept": "application/json"},
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
                kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
                return self._branch("providerRejected", kind)
            return body
        finally:
            await response.aclose()

    async def _access_token(
        self, call: Call, app_id: str, app_secret: str
    ) -> str | QueryAttempt[ListDepartmentsOutput]:
        digest = hashlib.sha256((app_id + "\0" + app_secret).encode()).digest()
        async with self.client._token_lock:
            if (
                self.client._cached_token
                and self.client._credential_digest == digest
                and time.monotonic() < self.client._token_expires_at
            ):
                return self.client._cached_token
            result = await self._response(
                call, "/openapi/auth/access_token/get", {"app_id": app_id, "app_secret": app_secret}
            )
            if not isinstance(result, bytes):
                return result
            try:
                wire = json.loads(result)
            except (UnicodeDecodeError, ValueError):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            if (
                not isinstance(wire, dict)
                or not isinstance(wire.get("code"), int)
                or isinstance(wire["code"], bool)
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            if wire["code"] != 0:
                return self._provider_code(wire["code"])
            data = wire.get("data")
            if not isinstance(data, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            token = data.get("access_token")
            expires_in = data.get("expires_in")
            if (
                not isinstance(token, str)
                or not _valid_credential(token)
                or not isinstance(expires_in, int)
                or isinstance(expires_in, bool)
                or not 180 <= expires_in <= 86400
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            self.client._cached_token = token
            self.client._credential_digest = digest
            self.client._token_expires_at = time.monotonic() + expires_in - 120
            return token

    def _provider_code(self, code: int) -> QueryAttempt[ListDepartmentsOutput]:
        if code == 1009:
            return self._retry(FailureKind.RATE_LIMIT)
        kind = FailureKind.AUTHENTICATION if code in (403, 1007, 1008) else FailureKind.PROVIDER_REJECTION
        return self._branch("providerRejected", kind)

    async def invoke(self, call: Call, input: ListDepartmentsInput) -> QueryAttempt[ListDepartmentsOutput]:
        """Resolve current credentials and read Go-compatible department results."""
        if not isinstance(input, ListDepartmentsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
            app_id, app_secret = credentials.app_id.reveal(), credentials.app_secret.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        if not _valid_credential(app_id) or not _valid_credential(app_secret):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        token = await self._access_token(call, app_id, app_secret)
        if not isinstance(token, str):
            return token
        result = await self._response(call, "/openapi/department/list", {"access_token": token})
        if not isinstance(result, bytes):
            return result
        try:
            wire = json.loads(result)
        except (UnicodeDecodeError, ValueError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if (
            not isinstance(wire, dict)
            or not isinstance(wire.get("code"), int)
            or isinstance(wire["code"], bool)
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        if wire["code"] != 0:
            return self._provider_code(wire["code"])
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or not isinstance(data.get("data_list"), list)
            or len(data["data_list"]) > 10000
        ):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        departments: list[DepartmentSummary] = []
        for item in data["data_list"]:
            if not isinstance(item, dict):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            identifier, name, parent = item.get("dept_id"), item.get("name"), item.get("parent_id", "")
            if (
                not isinstance(identifier, str)
                or not 0 < len(identifier) <= 256
                or not isinstance(name, str)
                or not 0 < len(name) <= 512
                or not isinstance(parent, str)
                or len(parent) > 256
            ):
                return self._branch("invalidResponse", FailureKind.PROTOCOL)
            departments.append(DepartmentSummary(id=identifier, name=name, parent_id=parent))
        return QueryAttempt.branch_result("listed", ListDepartmentsOutput(tuple(departments)), Receipt())
