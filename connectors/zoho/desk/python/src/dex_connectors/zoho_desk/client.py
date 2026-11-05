# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho Desk HTTP transport and operation outcome classification."""

from __future__ import annotations

import ipaddress
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote_plus, urlsplit

import httpx
from dex_connectors.sdk import Call, CredentialProvider, Failure, FailureKind, Receipt
from dex_connectors.sdk.providerhttp import (
    BodyTooLargeError,
    ProviderTransportError,
    new_provider_http_client,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials

_DATA_CENTERS = {
    "zoho-us-oauth": "desk.zoho.com", "zoho-eu-oauth": "desk.zoho.eu",
    "zoho-in-oauth": "desk.zoho.in", "zoho-au-oauth": "desk.zoho.com.au",
    "zoho-jp-oauth": "desk.zoho.jp", "zoho-ca-oauth": "desk.zohocloud.ca",
    "zoho-sa-oauth": "desk.zoho.sa", "zoho-sg-oauth": "desk.zoho.sg",
    "zoho-ae-oauth": "desk.zoho.ae",
}
_ID = re.compile(r"[0-9]{1,20}\Z")
_CREDITS = re.compile(r"[0-9]{1,12}\Z")
_ERROR_CODE = re.compile(r"[A-Z][A-Z0-9_]{0,63}\Z")
_FIELD_NAME = re.compile(r"/[A-Za-z][A-Za-z0-9_]{0,63}(?:/[A-Za-z0-9_]{1,64}){0,3}\Z")
_ERROR_TYPE = re.compile(r"[a-z][A-Za-z_]{0,31}\Z")


@dataclass(frozen=True)
class Exchange:
    """One classified provider exchange with no raw credential material."""

    outcome: str
    status: int = 0
    body: bytes = b""
    headers: httpx.Headers | None = None
    failure: Failure | None = None
    retry_after_seconds: float = 0


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build a secret-safe Zoho Desk failure."""
    return Failure(kind, "zoho-desk", operation, message)


def _loopback(host: str | None) -> bool:
    if host is None:
        return False
    if host.lower() == "localhost":
        return True
    try:
        return ipaddress.ip_address(host).is_loopback
    except ValueError:
        return False


def _safe_token(value: str) -> bool:
    return bool(value) and all(32 <= ord(character) < 127 for character in value)


def _summary(body: bytes) -> tuple[str, list[str]]:
    try:
        document = json.loads(body)
        if not isinstance(document, dict):
            return "", []
    except (ValueError, UnicodeDecodeError):
        return "", []
    code = document.get("errorCode")
    code = code if isinstance(code, str) and _ERROR_CODE.fullmatch(code) else ""
    errors = document.get("errors")
    details: list[str] = []
    if isinstance(errors, list):
        for item in errors:
            if not isinstance(item, dict):
                continue
            name, error_type = item.get("fieldName"), item.get("errorType")
            if not isinstance(name, str) or not _FIELD_NAME.fullmatch(name):
                continue
            if isinstance(error_type, str) and _ERROR_TYPE.fullmatch(error_type):
                name += "=" + error_type
            details.append(name)
    return code, sorted(details)[:5]


def _with_summary(message: str, body: bytes) -> str:
    code, details = _summary(body)
    parts = ([code] if code else []) + (["errors: " + ", ".join(details)] if details else [])
    return message + (" [" + "; ".join(parts) + "]" if parts else "")


def _retry_after(headers: httpx.Headers) -> float:
    value = headers.get("Retry-After", "").strip()
    if re.fullmatch(r"[0-9]+", value):
        seconds = int(value)
        if 0 < seconds <= (2**63 - 1) // 1_000_000_000:
            return float(seconds)
    return 0


class Client:
    """Resolve Zoho OAuth credentials at dispatch and send bounded Desk requests.

    The injected HTTP client remains owned by the caller. A default client is
    owned by this connector and closed by ``aclose``.
    """

    def __init__(self, config: Config, credentials: CredentialProvider[Credentials],
                 http_client: httpx.AsyncClient | None = None, api_base_url: str = "") -> None:
        if config is None or credentials is None:
            raise ValueError("Zoho Desk configuration and credentials are required")
        if not _ID.fullmatch(config.org_id.strip()):
            raise ValueError("Zoho Desk orgId must be the organization's numeric ID")
        if config.max_response_bytes <= 0:
            raise ValueError("Zoho Desk response limit must be positive")
        if api_base_url:
            parsed = urlsplit(api_base_url)
            if (parsed.scheme not in ("http", "https") or not parsed.hostname or
                (parsed.scheme != "https" and not _loopback(parsed.hostname)) or
                parsed.username or parsed.password or parsed.query or parsed.fragment):
                raise ValueError("Zoho Desk API base URL is invalid")
        self.config = config
        self.credentials = credentials
        self.api_base_url = api_base_url.rstrip("/")
        self.http_client = http_client or new_provider_http_client(10)
        self._owns_http_client = http_client is None

    def search_tickets(self) -> Any:
        """Return the searchTickets Query bound to this client."""
        from .operations import SearchTicketsOperation
        return SearchTicketsOperation(self)

    def get_ticket(self) -> Any:
        """Return the getTicket Query bound to this client."""
        from .operations import GetTicketOperation
        return GetTicketOperation(self)

    def create_ticket(self) -> Any:
        """Return the createTicket Mutation bound to this client."""
        from .operations import CreateTicketOperation
        return CreateTicketOperation(self)

    def update_ticket(self) -> Any:
        """Return the updateTicket Mutation bound to this client."""
        from .operations import UpdateTicketOperation
        return UpdateTicketOperation(self)

    def add_comment(self) -> Any:
        """Return the addComment Mutation bound to this client."""
        from .operations import AddCommentOperation
        return AddCommentOperation(self)

    async def aclose(self) -> None:
        """Close the default HTTP transport when the Worker stops."""
        if self._owns_http_client:
            await self.http_client.aclose()

    async def resolve(self, call: Call, operation: str) -> tuple[Credentials | None, str | None, str]:
        """Resolve generated OAuth credentials and the matching Desk data center."""
        try:
            credentials = await self.credentials.resolve(call)
        except Exception as error:
            from dex_connectors.sdk import ReauthorizationRequiredError
            return None, None, "rejected" if isinstance(error, ReauthorizationRequiredError) else "retry"
        if not isinstance(credentials, Credentials):
            return None, None, "defect"
        token = credentials.access_token.reveal() if credentials.access_token else ""
        host = _DATA_CENTERS.get(credentials.auth_method_id)
        if host is None or not _safe_token(token):
            return None, None, "defect"
        return credentials, self.api_base_url or f"https://{host}/api/v1", ""

    async def exchange(self, call: Call, operation: str, credentials: Credentials,
                       base_url: str, method: str, path: str,
                       query: dict[str, str] | None = None,
                       payload: dict[str, Any] | None = None) -> Exchange:
        """Send once, or refresh and resend once after HTTP 401."""
        target = base_url + path
        if query:
            target += "?" + "&".join(
                f"{_query_escape(key)}={_query_escape(value)}" for key, value in sorted(query.items())
            )
        for attempt in range(2):
            result = await self._exchange_once(operation, credentials, method, target, payload)
            if result.status != 401 or attempt or not hasattr(self.credentials, "resolve_after_rejection"):
                return result
            try:
                replacement = await self.credentials.resolve_after_rejection(call)
                if (not isinstance(replacement, Credentials) or
                    replacement.auth_method_id != credentials.auth_method_id or
                    not replacement.access_token or not _safe_token(replacement.access_token.reveal())):
                    return result
                credentials = replacement
            except Exception:
                return result
        return result

    async def _exchange_once(self, operation: str, credentials: Credentials, method: str,
                             target: str, payload: dict[str, Any] | None) -> Exchange:
        token = credentials.access_token.reveal() if credentials.access_token else ""
        headers = {"Authorization": "Zoho-oauthtoken " + token,
                   "orgId": self.config.org_id.strip(), "Accept": "application/json"}
        if payload is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, target, headers=headers,
                content=json.dumps(payload, ensure_ascii=False, separators=(",", ":")).encode() if payload is not None else None,
                timeout=10)
            response, _ = await send_traced(self.http_client, request)
            status = response.status_code
            response_headers = response.headers
            try:
                body = await read_bounded_body(response, self.config.max_response_bytes if 200 <= status < 300 else 65536)
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return Exchange("invalid", status, headers=response_headers,
                        failure=failure(operation, FailureKind.RESPONSE_TOO_LARGE,
                            "Zoho Desk response exceeds the configured maxResponseBytes limit"))
                body = b""
        except ProviderTransportError as error:
            outcome = "not_sent" if error.provably_not_sent else "unavailable"
            message = ("Zoho Desk could not be reached; no request was sent" if outcome == "not_sent"
                       else "Zoho Desk request failed before a response arrived")
            return Exchange(outcome, failure=failure(operation, FailureKind.TRANSPORT, message))
        except httpx.HTTPError:
            return Exchange("unavailable", failure=failure(operation, FailureKind.TRANSPORT,
                "Zoho Desk request failed before a response arrived"))
        if token.encode() in body:
            if 200 <= status < 300:
                return Exchange("invalid", status, headers=response_headers,
                    failure=failure(operation, FailureKind.PROTOCOL,
                        "Zoho Desk response reflected the connection credential"))
            body = b""
        if 200 <= status < 300:
            return Exchange("success", status, body, response_headers)
        code, _ = _summary(body)
        delay = _retry_after(response_headers)
        if status == 429:
            return Exchange("rate_limited", status, headers=response_headers, retry_after_seconds=delay,
                failure=failure(operation, FailureKind.RATE_LIMIT,
                    _with_summary("Zoho Desk rate limited the request (HTTP 429)", body)))
        if status == 408 or status >= 500:
            return Exchange("unavailable", status, headers=response_headers, retry_after_seconds=delay,
                failure=failure(operation, FailureKind.AVAILABILITY,
                    _with_summary(f"Zoho Desk could not complete the request (HTTP {status})", body)))
        if status == 404:
            return Exchange("not_found", status, headers=response_headers,
                failure=failure(operation, FailureKind.NOT_FOUND,
                    _with_summary("Zoho Desk found no such resource (HTTP 404)", body)))
        if 300 <= status < 400:
            kind, message = FailureKind.PROTOCOL, f"Zoho Desk redirected the request (HTTP {status}); check that the connection uses the account's data center"
        elif status == 401:
            kind, message = FailureKind.AUTHENTICATION, "Zoho Desk rejected the access token (HTTP 401); reconnect the connection"
        elif status == 403:
            kind = FailureKind.AUTHORIZATION
            message = {"OAUTH_ORG_MISMATCH": "Zoho Desk refused the request (HTTP 403): the token is bound to another organization; set orgId to the organization chosen at Connect, or reconnect",
                       "SCOPE_MISMATCH": "Zoho Desk refused the request (HTTP 403): the token lacks a required scope; reconnect and accept every requested scope"}.get(code, "Zoho Desk refused the request (HTTP 403)")
        else:
            kind = {400: FailureKind.VALIDATION, 422: FailureKind.VALIDATION,
                    409: FailureKind.CONFLICT}.get(status, FailureKind.PROVIDER_REJECTION)
            message = f"Zoho Desk rejected the request (HTTP {status})"
        return Exchange("rejected", status, headers=response_headers,
            failure=failure(operation, kind, _with_summary(message, body)))

    def receipt(self, call: Call, exchange: Exchange, object_id: str = "") -> Receipt:
        """Correlate a provider exchange with the Call ID and safe credit counts."""
        headers = exchange.headers or httpx.Headers()
        metadata = {}
        for source, target in (("X-Rate-Limit-Request-Weight-v3", "requestCredits"),
                               ("X-Rate-Limit-Remaining-v3", "remainingCredits")):
            value = headers.get(source, "")
            if _CREDITS.fullmatch(value):
                metadata[target] = value
        return Receipt(call.id, "zoho-desk", datetime.now(UTC), call.idempotency_key,
                       object_id, metadata)


def _query_escape(value: str) -> str:
    return quote_plus(value, safe="").replace("~", "%7E")
