# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Xero Accounting transport and response classification."""

from __future__ import annotations

import ipaddress
import json
import re
from collections.abc import Callable
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import quote, urlencode

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.credentials import ReauthorizationRequiredError
from dex_connectors.sdk.projectconfig.provider import ExchangePendingError, RefreshFailedError
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    read_error_tokens,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

if TYPE_CHECKING:
    from .operations import (
        CreateInvoiceOperation,
        FindContactByEmailOperation,
        GetInvoiceOperation,
        ListContactsOperation,
        ListInvoicesOperation,
        RecordPaymentOperation,
    )

_UUID = re.compile(r"[0-9a-fA-F]{8}(?:-[0-9a-fA-F]{4}){3}-[0-9a-fA-F]{12}\Z")
_RATE_COUNT = re.compile(r"[0-9]{1,10}\Z")
_API = "https://api.xero.com"


def _reject_nonfinite(value: str) -> object:
    raise ValueError("Xero returned a non-finite JSON number")


def is_uuid(value: str) -> bool:
    """Return whether a value has Xero's UUID wire shape."""
    return _UUID.fullmatch(value) is not None


def go_path_escape(value: str) -> str:
    """Escape a path segment with Go url.PathEscape's safe set."""
    return quote(value, safe="-._~$&+=:@")


def go_query(values: dict[str, str]) -> str:
    """Encode sorted query keys with Go url.Values.Encode semantics."""
    return urlencode(sorted(values.items()), safe="")


def equal_fold(left: str, right: str) -> bool:
    """Compare Unicode strings with Go's one-rune-at-a-time simple folding."""
    return len(left) == len(right) and all(
        first == second
        or (len(first.lower()) == len(second.lower()) == 1 and first.lower() == second.lower())
        or (len(first.upper()) == len(second.upper()) == 1 and first.upper() == second.upper())
        or (len(first.casefold()) == len(second.casefold()) == 1 and first.casefold() == second.casefold())
        for first, second in zip(left, right, strict=True)
    )


def _error_summary(body: bytes, token: str) -> str:
    parts: list[str] = []
    for pointers in (("/Type",), ("/ErrorNumber",), ("/Detail", "/detail", "/error")):
        matches = read_error_tokens(body, pointers)
        if (matches and (not token or token not in matches[0])
                and (pointers[0] != "/ErrorNumber" or _RATE_COUNT.fullmatch(matches[0]))):
            parts.append("error " + matches[0] if pointers[0] == "/ErrorNumber" else matches[0])
    try:
        document = json.loads(body)
        elements = document.get("Elements", []) if isinstance(document, dict) else []
        count = min(sum(
            len(element.get("ValidationErrors", []))
            for element in elements if isinstance(element, dict)
            and isinstance(element.get("ValidationErrors", []), list)
        ), 1000)
        if count:
            parts.append(f"{count} validation error" + ("s" if count != 1 else ""))
    except (ValueError, TypeError, AttributeError):
        pass
    return " [" + "; ".join(parts) + "]" if parts else ""


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-free Dex Failure."""
    return Failure(kind, "xero", operation, message)


@dataclass(frozen=True)
class Exchange:
    """Classified result of one provider request."""

    outcome: str
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: Failure | None = None
    retry_after: float = 0
    metadata: dict[str, str] | None = None


class Client:
    """Execute Xero calls using call-time credentials and a bounded HTTP transport."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        local_provider_url: str = "",
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Xero configuration and credential provider are required")
        if config.max_response_bytes <= 0:
            raise ValueError("Xero response limit must be positive")
        if len(config.organisation.strip()) > 255 or any(ord(ch) < 32 for ch in config.organisation):
            raise ValueError("Xero organisation is invalid")
        if local_provider_url:
            parsed = httpx.URL(local_provider_url)
            try:
                is_loopback = parsed.host == "localhost" or ipaddress.ip_address(parsed.host).is_loopback
            except ValueError:
                is_loopback = parsed.host == "localhost"
            if (
                not is_loopback
                or parsed.path not in ("", "/")
                or parsed.query
                or parsed.fragment
                or parsed.userinfo
            ):
                raise ValueError("local Xero provider must be a loopback origin")
        self.config = config
        self._credentials = credentials
        self._http_client = http_client or new_provider_http_client(20)
        self._owns_http_client = http_client is None
        self._local_provider_url = local_provider_url.rstrip("/")
        self._now = now or (lambda: datetime.now(UTC))
        self._derived_tenant = ""

    async def aclose(self) -> None:
        """Close the connector-owned HTTP client."""
        if self._owns_http_client:
            await self._http_client.aclose()

    def list_contacts(self) -> ListContactsOperation:
        """Return the listContacts Query."""
        from .operations import ListContactsOperation

        return ListContactsOperation(self)

    def find_contact_by_email(self) -> FindContactByEmailOperation:
        """Return the findContactByEmail Query."""
        from .operations import FindContactByEmailOperation

        return FindContactByEmailOperation(self)

    def get_invoice(self) -> GetInvoiceOperation:
        """Return the getInvoice Query."""
        from .operations import GetInvoiceOperation

        return GetInvoiceOperation(self)

    def list_invoices(self) -> ListInvoicesOperation:
        """Return the listInvoices Query."""
        from .operations import ListInvoicesOperation

        return ListInvoicesOperation(self)

    def create_invoice(self) -> CreateInvoiceOperation:
        """Return the createInvoice Mutation."""
        from .operations import CreateInvoiceOperation

        return CreateInvoiceOperation(self)

    def record_payment(self) -> RecordPaymentOperation:
        """Return the recordPayment Mutation."""
        from .operations import RecordPaymentOperation

        return RecordPaymentOperation(self)

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: bytes | None = None,
        modified_since: datetime | None = None,
        mutation: bool = False,
    ) -> Exchange:
        """Resolve generated OAuth credentials, select tenant, and send one Accounting request."""
        try:
            credentials = await self._credentials.resolve(call)
        except ReauthorizationRequiredError:
            return Exchange(
                "rejected",
                error=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Xero authorization must be renewed: reconnect the app or replace the client secret",
                ),
            )
        except (RefreshFailedError, ExchangePendingError):
            return Exchange(
                "retry",
                error=failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    "Xero access token request is temporarily unavailable",
                ),
            )
        except Exception:
            return Exchange(
                "defect",
                error=failure(
                    operation, FailureKind.AUTHENTICATION, "Xero connection credentials are unavailable"
                ),
            )
        if not isinstance(credentials, Credentials) or not credentials.access_token.reveal():
            return Exchange(
                "defect",
                error=failure(
                    operation, FailureKind.AUTHENTICATION, "Xero connection credentials are unavailable"
                ),
            )
        tenant_id = ""
        if credentials.auth_method_id == "xero-oauth":
            organisation = self.config.organisation.strip()
            if is_uuid(organisation):
                tenant_id = organisation.lower()
            elif self._derived_tenant:
                tenant_id = self._derived_tenant
            else:
                tenant_exchange = await self._send(
                    call,
                    operation,
                    credentials.access_token.reveal(),
                    "GET",
                    "/connections",
                    {},
                    None,
                    None,
                    False,
                    "",
                )
                if tenant_exchange.outcome in ("server", "invalid", "not_found"):
                    return Exchange(
                        "retry" if tenant_exchange.outcome == "server" else "rejected",
                        tenant_exchange.status,
                        tenant_exchange.headers,
                        error=tenant_exchange.error,
                        retry_after=tenant_exchange.retry_after,
                        metadata=tenant_exchange.metadata,
                    )
                if tenant_exchange.outcome != "success":
                    return tenant_exchange
                try:
                    connections = json.loads(
                        tenant_exchange.body, parse_constant=_reject_nonfinite
                    )
                except (ValueError, TypeError):
                    return Exchange(
                        "rejected",
                        tenant_exchange.status,
                        tenant_exchange.headers,
                        error=failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "Xero returned a connections list that is not a JSON array",
                        ),
                    )
                if not isinstance(connections, list):
                    return Exchange(
                        "rejected",
                        tenant_exchange.status,
                        tenant_exchange.headers,
                        error=failure(
                            operation,
                            FailureKind.PROTOCOL,
                            "Xero returned a connections list that is not a JSON array",
                        ),
                    )
                try:
                    candidates = [
                        entry
                        for entry in connections
                        if isinstance(entry, dict)
                        and entry.get("tenantType") == "ORGANISATION"
                        and isinstance(entry.get("tenantId"), str)
                        and is_uuid(entry["tenantId"])
                        and (
                            not organisation
                            or isinstance(entry.get("tenantName"), str)
                            and equal_fold(entry["tenantName"].strip(), organisation)
                        )
                    ]
                    if len(candidates) != 1:
                        raise ValueError("organisation selection is ambiguous or missing")
                    tenant_id = candidates[0]["tenantId"].lower()
                    self._derived_tenant = tenant_id
                except (ValueError, TypeError):
                    return Exchange(
                        "defect",
                        error=failure(
                            operation, FailureKind.VALIDATION, "Xero organisation could not be selected"
                        ),
                    )
        result = await self._send(
            call,
            operation,
            credentials.access_token.reveal(),
            method,
            path,
            query or {},
            payload,
            modified_since,
            mutation,
            tenant_id,
        )
        if result.status == 401 and hasattr(self._credentials, "resolve_after_rejection"):
            try:
                replacement = await self._credentials.resolve_after_rejection(call)
                if (
                    isinstance(replacement, Credentials)
                    and replacement.auth_method_id == credentials.auth_method_id
                ):
                    result = await self._send(
                        call,
                        operation,
                        replacement.access_token.reveal(),
                        method,
                        path,
                        query or {},
                        payload,
                        modified_since,
                        mutation,
                        tenant_id,
                    )
            except Exception:
                pass  # The original 401 remains the conclusive provider result.
        if result.status == 403 and tenant_id == self._derived_tenant:
            self._derived_tenant = ""
        if tenant_id:
            metadata = dict(result.metadata or {})
            metadata["tenantId"] = tenant_id
            result = Exchange(
                result.outcome,
                result.status,
                result.headers,
                result.body,
                result.error,
                result.retry_after,
                metadata,
            )
        return result

    async def _send(
        self,
        call: Call,
        operation: str,
        token: str,
        method: str,
        path: str,
        query: dict[str, str],
        payload: bytes | None,
        modified_since: datetime | None,
        mutation: bool,
        tenant_id: str,
    ) -> Exchange:
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        if tenant_id:
            headers["Xero-Tenant-Id"] = tenant_id
        if modified_since is not None:
            headers["If-Modified-Since"] = modified_since.astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%S")
        if mutation:
            key = str(call.idempotency_key or "")
            if not key or len(key.encode()) > 128:
                return Exchange(
                    "defect",
                    error=failure(
                        operation, FailureKind.LOCAL_DEFECT, "Xero mutation has no usable idempotency key"
                    ),
                )
            headers["Idempotency-Key"] = key
        target = (
            (self._local_provider_url or _API) + ("/api.xro/2.0" if path != "/connections" else "") + path
        )
        if query:
            target += "?" + go_query(query)
        try:
            request = self._http_client.build_request(method, target, headers=headers, content=payload)
            response, _ = await send_traced(self._http_client, request)
        except (httpx.TransportError, OSError):
            return Exchange(
                "retry",
                error=failure(
                    operation, FailureKind.TRANSPORT, "Xero request failed before a response arrived"
                ),
            )
        status = response.status_code
        metadata = {}
        for header, key in (
            ("X-DayLimit-Remaining", "dayLimitRemaining"),
            ("X-MinLimit-Remaining", "minuteLimitRemaining"),
            ("X-AppMinLimit-Remaining", "appMinuteLimitRemaining"),
        ):
            value = response.headers.get(header, "").strip()
            if _RATE_COUNT.fullmatch(value):
                metadata[key] = value
        if not 200 <= status < 300:
            chunks: list[bytes] = []
            remaining = MAX_ERROR_BODY_BYTES
            try:
                async for chunk in response.aiter_bytes():
                    chunks.append(chunk[:remaining])
                    remaining -= min(len(chunk), remaining)
                    if not remaining:
                        break
            except (httpx.TransportError, ProviderTransportError):
                chunks = []
            finally:
                await response.aclose()
            return self._classify_status(
                operation, status, response.headers, metadata, _error_summary(b"".join(chunks), token)
            )
        try:
            body = await read_bounded_body(response, self.config.max_response_bytes)
        except BodyTooLargeError:
            return Exchange(
                "invalid",
                status,
                response.headers,
                error=failure(
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Xero response exceeds the configured maxResponseBytes limit",
                ),
                metadata=metadata,
            )
        except ProviderTransportError:
            return Exchange(
                "retry",
                status,
                response.headers,
                error=failure(operation, FailureKind.TRANSPORT, "Xero response was interrupted"),
                metadata=metadata,
            )
        if token.encode() in body:
            return Exchange(
                "invalid",
                status,
                response.headers,
                error=failure(
                    operation, FailureKind.PROTOCOL, "Xero response reflected the connection credential"
                ),
                metadata=metadata,
            )
        return Exchange("success", status, response.headers, body, metadata=metadata)

    def _classify_status(
        self, operation: str, status: int, headers: httpx.Headers, metadata: dict[str, str], summary: str = ""
    ) -> Exchange:
        retry_value = headers.get("Retry-After", "").strip()
        retry_after = parse_retry_after(retry_value, self._now())
        messages = {
            500: ("server", FailureKind.AVAILABILITY, "Xero failed internally"),
            501: ("rejected", FailureKind.PROVIDER_REJECTION, "Xero does not implement the request"),
            401: ("rejected", FailureKind.AUTHENTICATION, "Xero rejected the access token"),
            403: ("rejected", FailureKind.AUTHORIZATION, "Xero denied access to the organisation or scope"),
            404: ("not_found", FailureKind.NOT_FOUND, "Xero found no such resource"),
            400: ("rejected", FailureKind.VALIDATION, "Xero rejected the request"),
        }
        if status == 429:
            problem = headers.get("X-Rate-Limit-Problem", "").strip()
            if re.fullmatch(r"[A-Za-z]{1,32}", problem):
                metadata["rateLimitProblem"] = problem.lower()
            daily = problem.lower() == "day" or _RATE_COUNT.fullmatch(retry_value) and int(retry_value) > 120
            if daily:
                if _RATE_COUNT.fullmatch(retry_value):
                    metadata["retryAfterSeconds"] = str(int(retry_value))
                return Exchange(
                    "daily",
                    status,
                    headers,
                    error=failure(
                        operation,
                        FailureKind.QUOTA_EXHAUSTED,
                        "Xero's daily API limit for the organisation is exhausted (HTTP 429)" + summary,
                    ),
                    metadata=metadata,
                )
            return Exchange(
                "retry",
                status,
                headers,
                error=failure(
                    operation,
                    FailureKind.RATE_LIMIT,
                    f"Xero's {problem.lower() or 'rate'} limit throttled the request (HTTP 429)" + summary,
                ),
                retry_after=retry_after,
                metadata=metadata,
            )
        if status in messages:
            outcome, kind, message = messages[status]
        elif status == 408 or status >= 500:
            outcome, kind, message = "retry", FailureKind.AVAILABILITY, "Xero is temporarily unavailable"
        elif 300 <= status < 400:
            outcome, kind, message = "rejected", FailureKind.PROTOCOL, "Xero redirected the request"
        else:
            outcome, kind, message = "rejected", FailureKind.PROVIDER_REJECTION, "Xero rejected the request"
        return Exchange(
            outcome,
            status,
            headers,
            error=failure(operation, kind, f"{message} (HTTP {status})" + summary),
            retry_after=retry_after,
            metadata=metadata,
        )

    def receipt(self, call: Call, result: Exchange, object_id: str = "") -> Receipt:
        """Record correlation and safe quota metadata without provider body text."""
        correlation = result.headers.get("Xero-Correlation-Id", "")
        if re.fullmatch(r"[A-Za-z0-9-]{1,64}", correlation) is None:
            correlation = ""
        return Receipt(
            call.id,
            "xero",
            self._now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            correlation,
            result.metadata or {},
        )
