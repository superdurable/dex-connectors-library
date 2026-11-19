# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Hesi token exchange and bounded department Query."""

import json
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlparse

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
from ._generated.definitions import LIST_DEPARTMENTS_DEFINITION
from .models import DepartmentSummary, ListDepartmentsInput, ListDepartmentsOutput

_AUTH_PATH = "/api/openapi/v1/auth/getAccessToken"
_DEPARTMENTS_PATH = "/api/openapi/v1/departments"
_ALLOWED_HOSTS = frozenset(
    {
        "app.ekuaibao.com",
        "dd2.hosecloud.com",
        "wx2.ekuaibao.com",
        "kdcloud2.ekuaibao.com",
        "ddenterprise.ekuaibao.com",
        "feishu.ekuaibao.com",
    }
)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListDepartmentsOutput]:
    return QueryAttempt.branch_result(
        branch, ListDepartmentsOutput(), Receipt(), Failure(kind, "hesi", "listDepartments", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListDepartmentsOutput]:
    return QueryAttempt.retry(Failure(kind, "hesi", "listDepartments", message), delay)


def _valid_credential(value: object) -> bool:
    return isinstance(value, str) and 0 < len(value) <= 2048 and all(0x21 <= ord(c) <= 0x7E for c in value)


def _decode_json(body: bytes) -> Any:
    return json.loads(body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError()))


class Client:
    """Own Hesi HTTP settings and resolve current credentials for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Hesi configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Hesi timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        parsed = urlparse(self.base_url)
        if (
            parsed.hostname not in _ALLOWED_HOSTS
            or parsed.port is not None
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("Hesi base URL must be a documented HTTPS platform origin")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_departments(self) -> "ListDepartmentsOperation":
        """Bind the department-list Query to this client."""
        return ListDepartmentsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class ListDepartmentsOperation:
    """Exchange AppKey and AppSecurity, then read one department page."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Hesi client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated department Query identity and retry policy."""
        return LIST_DEPARTMENTS_DEFINITION

    async def invoke(self, call: Call, input: ListDepartmentsInput) -> QueryAttempt[ListDepartmentsOutput]:
        """Make the token and department requests, keeping the token out of results."""
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key, app_security = credentials.app_key.reveal(), credentials.app_security.reveal()
            if not _valid_credential(app_key) or not _valid_credential(app_security):
                raise ValueError("invalid credentials")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Hesi credentials are unavailable or invalid"
            )
        count = input.count or 20
        if input.start < 0 or input.start > 1000000 or count < 1 or count > 100:
            return _branch("defect", FailureKind.VALIDATION, "start or count is outside the supported range")
        try:
            auth_request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + _AUTH_PATH,
                json={"appKey": app_key, "appSecurity": app_security},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "authorization request is invalid")
        auth_body, auth_attempt = await self._send(auth_request, "authorization")
        if auth_attempt is not None:
            return auth_attempt
        try:
            auth_wire = _decode_json(auth_body)
            token = auth_wire["value"]["accessToken"]
            if not _valid_credential(token):
                raise ValueError("invalid access token")
        except (ValueError, UnicodeDecodeError, KeyError, TypeError, IndexError):
            return _branch(
                "invalidResponse", FailureKind.PROTOCOL, "Hesi returned invalid authorization data"
            )
        try:
            department_request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + _DEPARTMENTS_PATH,
                params={"accessToken": token, "start": input.start, "count": count},
                headers={"Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "department request is invalid")
        body, attempt = await self._send(department_request, "department")
        if attempt is not None:
            return attempt
        return _decode_departments(body, input.start, count)

    async def _send(
        self,
        request: httpx.Request,
        stage: str,
    ) -> tuple[bytes, QueryAttempt[ListDepartmentsOutput] | None]:
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return b"", _retry(FailureKind.AVAILABILITY, f"Hesi {stage} transport failed")
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return b"", _retry(FailureKind.RATE_LIMIT, f"Hesi {stage} request rate limited", delay)
            if status == 408 or status >= 500:
                return b"", _retry(FailureKind.AVAILABILITY, f"Hesi {stage} temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return b"", _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    f"Hesi {stage} response exceeded the configured limit",
                )
            except ProviderTransportError:
                return b"", _retry(FailureKind.AVAILABILITY, f"Hesi {stage} response was interrupted")
            if status != 200:
                kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
                return b"", _branch("providerRejected", kind, f"Hesi rejected {stage} with HTTP {status}")
            return body, None
        finally:
            await response.aclose()


def _decode_departments(body: bytes, start: int, count: int) -> QueryAttempt[ListDepartmentsOutput]:
    try:
        wire = _decode_json(body)
        total, items = wire["count"], wire["items"]
        if type(total) is not int or total < 0 or not isinstance(items, list) or len(items) > count:
            raise ValueError("invalid page")
        departments = []
        for item in items:
            department_id, name, code, active = item["id"], item["name"], item.get("code", ""), item["active"]
            if (
                not isinstance(department_id, str)
                or not 0 < len(department_id) <= 256
                or not isinstance(name, str)
                or not 0 < len(name) <= 512
                or not isinstance(code, str)
                or len(code) > 256
                or type(active) is not bool
            ):
                raise ValueError("invalid department")
            departments.append(DepartmentSummary(department_id, name, code, active))
        next_start = 0
        if start + len(departments) < total:
            if not departments or start + len(departments) > 1000000:
                raise ValueError("pagination cannot advance")
            next_start = start + len(departments)
    except (ValueError, UnicodeDecodeError, KeyError, TypeError, IndexError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "Hesi returned invalid department data")
    return QueryAttempt.branch_result(
        "listed", ListDepartmentsOutput(tuple(departments), total, next_start), Receipt()
    )
