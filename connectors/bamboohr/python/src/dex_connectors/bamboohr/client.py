# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded BambooHR API transport and response classification."""

import base64
import re
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        AddEmployeeOperation,
        FindEmployeeByEmailOperation,
        GetEmployeeOperation,
        ListEmployeeChangesOperation,
        ListTimeOffRequestsOperation,
        UpdateEmployeeOperation,
    )

_COMPANY_DOMAIN = re.compile(r"^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$")
_ERROR_CODE = re.compile(r"^[A-Za-z][A-Za-z0-9_.-]{0,63}$")


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider response classified before operation-specific routing."""

    outcome: str
    status_code: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    failure: Failure | None = None
    retry_after: float = 0


def bamboohr_failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Create a persistable failure without provider free text or credentials."""
    return Failure(kind, "bamboohr", operation, message)


class Client:
    """Execute BambooHR API v1 operations with a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("BambooHR configuration and credentials are required")
        if not _COMPANY_DOMAIN.fullmatch(config.company_domain):
            raise ValueError("BambooHR companyDomain must be lowercase letters, digits, and hyphens")
        if config.max_response_bytes < 1:
            raise ValueError("BambooHR response limit must be positive")
        endpoint = validate_base_url(api_base_url or f"https://{config.company_domain}.bamboohr.com/api/v1")
        self.config = config
        self._endpoint = endpoint.rstrip("/")
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(9)
        self._owns_http_client = http_client is None

    async def aclose(self) -> None:
        """Close the connector-owned HTTP transport after Worker shutdown."""
        if self._owns_http_client:
            await self._http_client.aclose()

    def get_employee(self) -> "GetEmployeeOperation":
        """Bind the getEmployee Query to this client."""
        from .operations import GetEmployeeOperation

        return GetEmployeeOperation(self)

    def find_employee_by_email(self) -> "FindEmployeeByEmailOperation":
        """Bind the findEmployeeByEmail Query to this client."""
        from .operations import FindEmployeeByEmailOperation

        return FindEmployeeByEmailOperation(self)

    def list_employee_changes(self) -> "ListEmployeeChangesOperation":
        """Bind the listEmployeeChanges Query to this client."""
        from .operations import ListEmployeeChangesOperation

        return ListEmployeeChangesOperation(self)

    def list_time_off_requests(self) -> "ListTimeOffRequestsOperation":
        """Bind the listTimeOffRequests Query to this client."""
        from .operations import ListTimeOffRequestsOperation

        return ListTimeOffRequestsOperation(self)

    def update_employee(self) -> "UpdateEmployeeOperation":
        """Bind the updateEmployee Mutation to this client."""
        from .operations import UpdateEmployeeOperation

        return UpdateEmployeeOperation(self)

    def add_employee(self) -> "AddEmployeeOperation":
        """Bind the addEmployee Mutation to this client."""
        from .operations import AddEmployeeOperation

        return AddEmployeeOperation(self)

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Failure | None]:
        """Resolve and validate the current API key without retaining it in state."""
        try:
            credentials = await self._credentials.resolve(call)
            key = credentials.api_key.reveal()
            if (
                not key
                or not key.isascii()
                or any(ord(character) < 33 or ord(character) > 126 for character in key)
                or ":" in key
            ):
                raise ValueError("invalid BambooHR credential")
            return credentials, None
        except Exception:
            return None, bamboohr_failure(
                operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: dict[str, str] | None = None,
    ) -> Exchange:
        """Send one request, classify status, and discard untrusted error text."""
        key = credentials.api_key.reveal()
        authorization = base64.b64encode(f"{key}:x".encode()).decode("ascii")
        headers = {"Authorization": f"Basic {authorization}", "Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self._http_client.build_request(
                method,
                self._endpoint + path,
                params=query,
                json=payload,
                headers=headers,
            )
            response, _ = await send_traced(self._http_client, request)
            try:
                body = await read_bounded_body(response, self.config.max_response_bytes)
            except BodyTooLargeError:
                if 200 <= response.status_code < 300:
                    return Exchange(
                        "invalid",
                        response.status_code,
                        response.headers,
                        failure=bamboohr_failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "BambooHR response exceeds the configured maxResponseBytes limit",
                        ),
                    )
                body = b""
            if 200 <= response.status_code < 300:
                if key.encode() in body:
                    return Exchange(
                        "invalid",
                        response.status_code,
                        response.headers,
                        failure=bamboohr_failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "BambooHR response reflected the connection credential",
                        ),
                    )
                return Exchange("succeeded", response.status_code, response.headers, body)
            return self._classify_status(operation, response.status_code, response.headers, body, key)
        except ProviderTransportError as error:
            outcome = "not_sent" if error.provably_not_sent else "unavailable"
            message = (
                "BambooHR could not be reached; no request was sent"
                if error.provably_not_sent
                else "BambooHR request failed before a response arrived"
            )
            return Exchange(outcome, failure=bamboohr_failure(operation, FailureKind.TRANSPORT, message))
        except ValueError:
            return Exchange(
                "defect",
                failure=bamboohr_failure(
                    operation, FailureKind.LOCAL_DEFECT, "BambooHR request could not be built"
                ),
            )

    def _classify_status(
        self, operation: str, status: int, headers: httpx.Headers, body: bytes, key: str
    ) -> Exchange:
        error_code = ""
        for pointer in ("/error/code", "/code"):
            tokens = read_error_tokens(body, (pointer,))
            if len(tokens) == 1 and _ERROR_CODE.fullmatch(tokens[0]) and key not in tokens[0]:
                error_code = tokens[0]
                break
        suffix = f" [{error_code}]" if error_code else ""
        retry_after = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
        if status == 429:
            return Exchange(
                "rate_limited",
                status,
                headers,
                failure=bamboohr_failure(
                    operation, FailureKind.RATE_LIMIT, f"BambooHR rate limited the request (HTTP 429){suffix}"
                ),
                retry_after=retry_after,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "unavailable",
                status,
                headers,
                failure=bamboohr_failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"BambooHR could not complete the request (HTTP {status}){suffix}",
                ),
                retry_after=retry_after,
            )
        if status == 404:
            return Exchange(
                "not_found",
                status,
                headers,
                failure=bamboohr_failure(
                    operation, FailureKind.NOT_FOUND, f"BambooHR found no such resource (HTTP 404){suffix}"
                ),
            )
        if 300 <= status < 400:
            message = (
                f"BambooHR redirected the request (HTTP {status}); check that companyDomain "
                "is the mycompany in https://mycompany.bamboohr.com"
            )
            return Exchange(
                "rejected",
                status,
                headers,
                failure=bamboohr_failure(operation, FailureKind.PROTOCOL, message),
            )
        kind = {
            401: FailureKind.AUTHENTICATION,
            403: FailureKind.AUTHORIZATION,
            409: FailureKind.CONFLICT,
            400: FailureKind.VALIDATION,
            406: FailureKind.VALIDATION,
            422: FailureKind.VALIDATION,
        }.get(status, FailureKind.PROVIDER_REJECTION)
        return Exchange(
            "rejected",
            status,
            headers,
            failure=bamboohr_failure(
                operation, kind, f"BambooHR rejected the request (HTTP {status}){suffix}"
            ),
        )

    def receipt(self, call: Call, employee_id: str = "") -> Receipt:
        """Return a secret-safe correlation receipt for the current attempt."""
        return Receipt(call.id, "bamboohr", datetime.now(UTC), call.idempotency_key, employee_id)
