# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated, bounded Zoho CRM HTTP exchange and operation bindings."""

from __future__ import annotations

import asyncio
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    CredentialRefreshDriver,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
    RejectedCredentialRefreshingProvider,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
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
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

_DATA_CENTER_SUFFIXES = {
    "zoho-us-oauth": "zohoapis.com",
    "zoho-eu-oauth": "zohoapis.eu",
    "zoho-in-oauth": "zohoapis.in",
    "zoho-au-oauth": "zohoapis.com.au",
    "zoho-jp-oauth": "zohoapis.jp",
    "zoho-ca-oauth": "zohoapis.ca",
    "zoho-sa-oauth": "zohoapis.sa",
}
_SCOPE_BY_OPERATION = {
    "findRecords": "ZohoCRM.coql.READ or ZohoCRM.modules.READ",
    "listModifiedRecords": "ZohoCRM.coql.READ or ZohoCRM.modules.READ",
    "getRecord": "ZohoCRM.modules.READ",
    "upsertRecord": "ZohoCRM.modules.CREATE",
    "updateRecord": "ZohoCRM.modules.UPDATE",
    "listModuleFields": "ZohoCRM.settings.fields.READ",
}
_PERMISSION_CODES = {"OAUTH_SCOPE_MISMATCH", "NO_PERMISSION", "AUTHORIZATION_FAILED"}
_REQUEST_REJECTION_CODES = _PERMISSION_CODES | {
    "INVALID_MODULE",
    "NOT_SUPPORTED",
    "INVALID_URL_PATTERN",
    "INVALID_REQUEST_METHOD",
}
_ERROR_CODE = re.compile(r"^[A-Z][A-Z0-9_]{0,63}$")
_CREDITS = re.compile(r"^[0-9]{1,12}$")
_RECORD_ID = re.compile(r"^[0-9]{1,20}$")
_FIELD = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,99}$")
_FIELD_PATH = re.compile(r"^[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}$")


class _ProjectHTTPClient(httpx.AsyncClient):
    """Connection-owned HTTP client with Zoho's per-request timeout."""


@dataclass(frozen=True, slots=True)
class ErrorSummary:
    """Safe provider code and identifiers without echoed field values."""

    code: str = ""
    field: str = ""
    duplicate_record_id: str = ""
    has_details_id: bool = False

    @property
    def is_record_id_invalid(self) -> bool:
        """Identify Zoho's missing record response."""
        return self.code == "INVALID_DATA" and (
            self.field == "id" or (not self.field and self.has_details_id)
        )

    def provider_errors(self) -> tuple[Any, ...]:
        """Return the generated public error shape for definite record errors."""
        from .models import ProviderError

        return (ProviderError(self.code, self.field, self.duplicate_record_id),) if self.code else ()


def _error_summary(value: object) -> ErrorSummary:
    if not isinstance(value, dict):
        return ErrorSummary()
    code = value.get("code")
    if not isinstance(code, str) or not _ERROR_CODE.fullmatch(code):
        return ErrorSummary()
    details = value.get("details")
    details = details if isinstance(details, dict) else {}
    field = details.get("api_name")
    field = field if isinstance(field, str) and _FIELD_PATH.fullmatch(field) else ""
    duplicate = details.get("duplicate_record")
    duplicate = duplicate.get("id") if isinstance(duplicate, dict) else ""
    duplicate = duplicate if isinstance(duplicate, str) and _RECORD_ID.fullmatch(duplicate) else ""
    return ErrorSummary(code, field, duplicate, details.get("id") is not None)


def _reject_json_constant(_: str) -> None:
    raise ValueError("Zoho CRM error body contains a non-finite number")


def describe_error(body: bytes) -> ErrorSummary:
    """Read only Zoho's machine-readable error code and identifiers."""
    try:
        document = json.loads(body, parse_constant=_reject_json_constant)
    except (UnicodeDecodeError, ValueError):
        return ErrorSummary()
    if not isinstance(document, dict):
        return ErrorSummary()
    summary = _error_summary(document)
    if summary.code:
        return summary
    rows = document.get("data")
    if isinstance(rows, list):
        for row in rows:
            if isinstance(row, dict) and row.get("status") == "error":
                summary = _error_summary(row)
                if summary.code:
                    return summary
    return ErrorSummary()


@dataclass(frozen=True, slots=True)
class Exchange:
    """One classified provider exchange with safe response metadata."""

    outcome: str
    status: int = 0
    headers: httpx.Headers | None = None
    body: bytes = b""
    summary: ErrorSummary = ErrorSummary()
    failure: Failure | None = None
    retry_after: float = 0
    remaining_credits: str = ""


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a secret-safe Zoho CRM failure."""
    return Failure(kind, "zoho-crm", operation, message)


class Client:
    """Resolve current OAuth credentials and execute six Zoho CRM operations."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        api_base_url: str = "",
        refresh_driver: CredentialRefreshDriver[Credentials] | None = None,
    ) -> None:
        if config is None or type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Zoho CRM response limit must be positive")
        if credentials is None:
            raise ValueError("Zoho CRM credential provider is required")
        validated_base_url = validate_base_url(api_base_url) if api_base_url else ""
        self.config = config
        self.credentials = credentials
        self._owns_http_client = http_client is None or isinstance(http_client, _ProjectHTTPClient)
        self.http_client = http_client or new_provider_http_client(10)
        self.refresh_driver = refresh_driver or ManifestCredentialRefreshDriver[Credentials](
            "Zoho CRM", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
        )
        self.api_base_url_override = validated_base_url

    async def aclose(self) -> None:
        """Close the HTTP client only when this connector created it."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def find_records(self) -> Any:
        """Bind the findRecords Query."""
        from .operations import FindRecordsOperation

        return FindRecordsOperation(self)

    def get_record(self) -> Any:
        """Bind the getRecord Query."""
        from .operations import GetRecordOperation

        return GetRecordOperation(self)

    def list_modified_records(self) -> Any:
        """Bind the listModifiedRecords Query."""
        from .operations import ListModifiedRecordsOperation

        return ListModifiedRecordsOperation(self)

    def upsert_record(self) -> Any:
        """Bind the upsertRecord Mutation."""
        from .operations import UpsertRecordOperation

        return UpsertRecordOperation(self)

    def update_record(self) -> Any:
        """Bind the updateRecord Mutation."""
        from .operations import UpdateRecordOperation

        return UpdateRecordOperation(self)

    def list_module_fields(self) -> Any:
        """Bind the listModuleFields Query."""
        from .operations import ListModuleFieldsOperation

        return ListModuleFieldsOperation(self)

    def _api_base_url(self, credentials: Credentials) -> str:
        suffix = _DATA_CENTER_SUFFIXES.get(credentials.auth_method_id)
        if suffix is None:
            raise ValueError("the authorization method is not a supported data center")
        token = credentials.access_token.reveal() if credentials.access_token is not None else ""
        if not is_header_safe_credential(token):
            raise ValueError("the access token is missing or is not a valid header value")
        if self.api_base_url_override:
            return self.api_base_url_override
        domain = credentials.api_domain or f"https://www.{suffix}"
        parsed = urlsplit(domain)
        if (
            parsed.scheme != "https"
            or parsed.username is not None
            or parsed.password is not None
            or parsed.port is not None
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.hostname not in {f"{prefix}.{suffix}" for prefix in ("www", "sandbox", "developer")}
        ):
            raise ValueError("Zoho CRM api_domain is not a zohoapis host of the connection's data center")
        return f"https://{parsed.hostname}/crm/v8"

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        *,
        query: str = "",
        payload: object | None = None,
        deadline_at: float | None = None,
    ) -> Exchange:
        """Bound credential refresh and every resend by one operation deadline."""
        loop = asyncio.get_running_loop()
        remaining = (deadline_at if deadline_at is not None else loop.time() + 25) - loop.time()
        try:
            return await asyncio.wait_for(
                self._exchange_with_refresh(call, operation, method, path, query, payload),
                timeout=max(0, remaining),
            )
        except TimeoutError:
            return Exchange(
                "retry",
                failure=failure(
                    operation,
                    FailureKind.TRANSPORT,
                    "the operation deadline passed before Zoho CRM answered",
                ),
            )

    async def _exchange_with_refresh(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: str,
        payload: object | None,
    ) -> Exchange:
        """Resolve a token, send once, and refresh after one conclusive 401."""
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ReauthorizationRequiredError:
            return Exchange(
                "rejected",
                failure=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Zoho authorization is revoked or expired; reconnect the connection",
                ),
            )
        except Exception:
            return Exchange(
                "retry",
                failure=failure(
                    operation, FailureKind.AUTHENTICATION, "Zoho credentials are temporarily unavailable"
                ),
            )
        try:
            base_url = self._api_base_url(credentials)
        except ValueError as error:
            return Exchange(
                "defect",
                failure=failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Zoho CRM connection credentials are missing or invalid: " + str(error),
                ),
            )
        for attempt in range(2):
            result = await self._exchange_once(
                call, operation, credentials, base_url, method, path, query, payload
            )
            if result.status != 401 or result.summary.code == "OAUTH_SCOPE_MISMATCH" or attempt:
                return result
            if not isinstance(self.credentials, RejectedCredentialRefreshingProvider):
                return result
            try:
                replacement = await resolve_credential_after_rejection(
                    self.credentials, call, self.refresh_driver
                )
                if replacement.auth_method_id != credentials.auth_method_id:
                    return result
                base_url = self._api_base_url(replacement)
            except Exception:
                return result
            credentials = replacement
        return result

    async def _exchange_once(
        self,
        call: Call,
        operation: str,
        credentials: Credentials,
        base_url: str,
        method: str,
        path: str,
        query: str,
        payload: object | None,
    ) -> Exchange:
        url = base_url + path + ("?" + query if query else "")
        assert credentials.access_token is not None
        headers = {
            "Authorization": "Zoho-oauthtoken " + credentials.access_token.reveal(),
            "Accept": "application/json",
        }
        try:
            content = (
                None
                if payload is None
                else json.dumps(payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
            )
        except (TypeError, ValueError):
            return Exchange(
                "defect",
                failure=failure(operation, FailureKind.LOCAL_DEFECT, "Zoho CRM request could not be built"),
            )
        if content is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, url, content=content, headers=headers)
            async with asyncio.timeout(25):
                response, _ = await send_traced(self.http_client, request)
                status = response.status_code
                response_headers = response.headers.copy()
                credits = response_headers.get("X-API-CREDITS-REMAINING", "")
                credits = credits if _CREDITS.fullmatch(credits) else ""
                limit = self.config.max_response_bytes if 200 <= status < 300 else 65536
                try:
                    body = await read_bounded_body(response, limit)
                except BodyTooLargeError:
                    if 200 <= status < 300:
                        return Exchange(
                            "invalid",
                            status,
                            response_headers,
                            failure=failure(
                                operation,
                                FailureKind.RESPONSE_TOO_LARGE,
                                "Zoho CRM response exceeds the configured maxResponseBytes limit",
                            ),
                            remaining_credits=credits,
                        )
                    body = b""
                if credentials.access_token.reveal().encode() in body:
                    if 200 <= status < 300:
                        return Exchange(
                            "invalid",
                            status,
                            response_headers,
                            failure=failure(
                                operation,
                                FailureKind.PROTOCOL,
                                "Zoho CRM response reflected the connection credential",
                            ),
                            remaining_credits=credits,
                        )
                    body = b""
                if 200 <= status < 300:
                    return Exchange("succeeded", status, response_headers, body, remaining_credits=credits)
                summary = describe_error(body)
                return self._classify_status(operation, status, response_headers, summary, credits)
        except (TimeoutError, ProviderTransportError, httpx.HTTPError):
            return Exchange(
                "retry",
                failure=failure(
                    operation, FailureKind.TRANSPORT, "Zoho CRM request failed before a response arrived"
                ),
            )

    def _classify_status(
        self,
        operation: str,
        status: int,
        headers: httpx.Headers,
        summary: ErrorSummary,
        credits: str,
    ) -> Exchange:
        delay = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
        if status == 429:
            return Exchange(
                "retry",
                status,
                headers,
                summary=summary,
                failure=failure(
                    operation, FailureKind.RATE_LIMIT, "Zoho CRM rate limited the request (HTTP 429)"
                ),
                retry_after=delay,
                remaining_credits=credits,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "retry",
                status,
                headers,
                summary=summary,
                failure=failure(
                    operation,
                    FailureKind.AVAILABILITY,
                    f"Zoho CRM could not complete the request (HTTP {status})",
                ),
                retry_after=delay,
                remaining_credits=credits,
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                status,
                headers,
                summary=summary,
                failure=failure(
                    operation,
                    FailureKind.PROTOCOL,
                    f"Zoho CRM redirected the request (HTTP {status}); "
                    "check that the connection uses the account's data center",
                ),
                remaining_credits=credits,
            )
        kind = FailureKind.PROVIDER_REJECTION
        if summary.code == "OAUTH_SCOPE_MISMATCH":
            kind = FailureKind.AUTHORIZATION
        elif status == 401:
            kind = FailureKind.AUTHENTICATION
        elif status == 403 or summary.code in _PERMISSION_CODES:
            kind = FailureKind.AUTHORIZATION
        elif summary.code == "DUPLICATE_DATA":
            kind = FailureKind.CONFLICT
        elif summary.is_record_id_invalid or status == 404:
            kind = FailureKind.NOT_FOUND
        elif status in (400, 422) or 200 <= status < 300:
            kind = FailureKind.VALIDATION
        return Exchange(
            "rejected",
            status,
            headers,
            summary=summary,
            failure=failure(operation, kind, f"Zoho CRM rejected the request (HTTP {status})"),
            remaining_credits=credits,
        )

    def classify_write_exchange(self, operation: str, exchange: Exchange) -> Exchange:
        """Treat an error row in a 2xx write response as a definite rejection."""
        if exchange.outcome != "succeeded":
            return exchange
        summary = describe_error(exchange.body)
        if not summary.code:
            return exchange
        return self._classify_status(
            operation,
            exchange.status,
            exchange.headers or httpx.Headers(),
            summary,
            exchange.remaining_credits,
        )

    def receipt(self, call: Call, exchange: Exchange, record_id: str = "") -> Receipt:
        """Record call identity, provider record, and safe credit metadata."""
        return Receipt(
            call.id,
            "zoho-crm",
            datetime.now(UTC),
            call.idempotency_key,
            record_id,
            metadata={"remainingCredits": exchange.remaining_credits} if exchange.remaining_credits else {},
        )
