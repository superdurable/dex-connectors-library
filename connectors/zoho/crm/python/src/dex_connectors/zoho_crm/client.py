# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Zoho CRM HTTP transport and Go-compatible response classification."""

import asyncio
import json
import re
import time
from dataclasses import dataclass, field
from datetime import UTC, datetime
from typing import TYPE_CHECKING, Any
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
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

if TYPE_CHECKING:
    from .operations import (
        FindRecordsOperation,
        GetRecordOperation,
        ListModifiedRecordsOperation,
        ListModuleFieldsOperation,
        UpdateRecordOperation,
        UpsertRecordOperation,
    )

_DATA_CENTERS = {
    "zoho-us-oauth": "com",
    "zoho-eu-oauth": "eu",
    "zoho-in-oauth": "in",
    "zoho-au-oauth": "com.au",
    "zoho-jp-oauth": "jp",
    "zoho-ca-oauth": "ca",
    "zoho-sa-oauth": "sa",
}
_ERROR_CODE = re.compile(r"[A-Z][A-Z0-9_]{0,63}\Z")
_FIELD_PATH = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,99}(?:\.[A-Za-z][A-Za-z0-9_]{0,99}){0,2}\Z")
_RECORD_ID = re.compile(r"[0-9]{1,20}\Z")
_CREDITS = re.compile(r"[0-9]{1,12}\Z")
_PERMISSION_CODES = {"OAUTH_SCOPE_MISMATCH", "NO_PERMISSION", "AUTHORIZATION_FAILED"}
_REQUEST_CODES = _PERMISSION_CODES | {
    "INVALID_MODULE",
    "NOT_SUPPORTED",
    "INVALID_URL_PATTERN",
    "INVALID_REQUEST_METHOD",
}
_SCOPES = {
    "findRecords": "ZohoCRM.coql.READ or ZohoCRM.modules.READ",
    "listModifiedRecords": "ZohoCRM.coql.READ or ZohoCRM.modules.READ",
    "getRecord": "ZohoCRM.modules.READ",
    "upsertRecord": "ZohoCRM.modules.CREATE",
    "updateRecord": "ZohoCRM.modules.UPDATE",
    "listModuleFields": "ZohoCRM.settings.fields.READ",
}


@dataclass(frozen=True, slots=True)
class ErrorSummary:
    """Safe identifiers from a Zoho error, excluding provider messages."""

    code: str = ""
    field: str = ""
    duplicate_record_id: str = ""
    has_details_id: bool = False

    def record_id_invalid(self) -> bool:
        """Recognize INVALID_DATA about the requested ID."""
        return self.code == "INVALID_DATA" and (
            self.field == "id" or (not self.field and self.has_details_id)
        )

    def describe(self, message: str) -> str:
        """Append only safe machine-readable identifiers."""
        details = "; ".join(
            filter(
                None,
                (
                    self.code,
                    f"field: {self.field}" if self.field else "",
                    f"duplicate record: {self.duplicate_record_id}" if self.duplicate_record_id else "",
                ),
            )
        )
        return message + (f" [{details}]" if details else "")


def _summarize_error(document: Any) -> ErrorSummary:
    if not isinstance(document, dict):
        return ErrorSummary()
    code = document.get("code")
    if not isinstance(code, str) or not _ERROR_CODE.fullmatch(code):
        return ErrorSummary()
    details = document.get("details")
    if not isinstance(details, dict):
        details = {}
    field = details.get("api_name")
    duplicate = details.get("duplicate_record")
    duplicate_id = duplicate.get("id") if isinstance(duplicate, dict) else None
    return ErrorSummary(
        code,
        field if isinstance(field, str) and _FIELD_PATH.fullmatch(field) else "",
        duplicate_id if isinstance(duplicate_id, str) and _RECORD_ID.fullmatch(duplicate_id) else "",
        "id" in details and details["id"] is not None,
    )


def describe_error(body: bytes) -> ErrorSummary:
    """Read request or first record error without retaining provider text."""
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ErrorSummary()
    summary = _summarize_error(document)
    if summary.code:
        return summary
    data = document.get("data") if isinstance(document, dict) else None
    if isinstance(data, list):
        for item in data:
            if isinstance(item, dict) and item.get("status") == "error":
                summary = _summarize_error(item)
                if summary.code:
                    return summary
    return ErrorSummary()


@dataclass(frozen=True, slots=True)
class Exchange:
    """One provider exchange with a safe response classification."""

    outcome: str
    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    summary: ErrorSummary = ErrorSummary()
    failure: Failure | None = None
    retry_after: float = 0
    remaining_credits: str = ""
    is_session_failure: bool = False


def failure(kind: FailureKind, operation: str, message: str) -> Failure:
    """Create a connector-owned, secret-safe failure."""
    return Failure(kind, "zoho-crm", operation, message)


class Client:
    """Resolve call-time credentials and execute bounded Zoho CRM requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        api_base_url: str = "",
    ) -> None:
        if credentials is None:
            raise ValueError("Zoho CRM credential provider is required")
        if config.max_response_bytes < 1:
            raise ValueError("Zoho CRM response limit must be positive")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(10)
        self._owns_http_client = http_client is None
        self.refresh_http_client = http_client or new_provider_http_client(8)
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Zoho",
            AUTHORIZATION,
            CREDENTIAL_FIELDS,
            self.refresh_http_client,
        )
        self.api_base_url_override = validate_base_url(api_base_url) if api_base_url else ""

    async def aclose(self) -> None:
        """Close the owned provider client."""
        if self._owns_http_client:
            await self.http_client.aclose()
            await self.refresh_http_client.aclose()

    def find_records(self) -> "FindRecordsOperation":
        """Bind the findRecords Query."""
        from .operations import FindRecordsOperation

        return FindRecordsOperation(self)

    def get_record(self) -> "GetRecordOperation":
        """Bind the getRecord Query."""
        from .operations import GetRecordOperation

        return GetRecordOperation(self)

    def list_modified_records(self) -> "ListModifiedRecordsOperation":
        """Bind the listModifiedRecords Query."""
        from .operations import ListModifiedRecordsOperation

        return ListModifiedRecordsOperation(self)

    def upsert_record(self) -> "UpsertRecordOperation":
        """Bind the upsertRecord Mutation."""
        from .operations import UpsertRecordOperation

        return UpsertRecordOperation(self)

    def update_record(self) -> "UpdateRecordOperation":
        """Bind the updateRecord Mutation."""
        from .operations import UpdateRecordOperation

        return UpdateRecordOperation(self)

    def list_module_fields(self) -> "ListModuleFieldsOperation":
        """Bind the listModuleFields Query."""
        from .operations import ListModuleFieldsOperation

        return ListModuleFieldsOperation(self)

    def _api_base_url(self, credentials: Credentials) -> str:
        suffix = _DATA_CENTERS.get(credentials.auth_method_id)
        if suffix is None:
            raise ValueError("the authorization method is not a supported data center")
        token = credentials.access_token.reveal() if credentials.access_token is not None else ""
        if not is_header_safe_credential(token):
            raise ValueError("the access token is missing or is not a valid header value")
        if self.api_base_url_override:
            return self.api_base_url_override
        domain = credentials.api_domain or f"https://www.zohoapis.{suffix}"
        try:
            parsed = urlsplit(domain)
            port = parsed.port
        except ValueError as error:
            raise ValueError("Zoho CRM api_domain must be an https zohoapis host of the connection's data center") from error
        if (
            parsed.scheme != "https"
            or port is not None
            or parsed.path not in ("", "/")
            or parsed.query
            or parsed.fragment
            or parsed.username
        ):
            raise ValueError("Zoho CRM api_domain must be an https zohoapis host of the connection's data center")
        allowed_hosts = {
            f"www.zohoapis.{suffix}", f"sandbox.zohoapis.{suffix}", f"developer.zohoapis.{suffix}",
        }
        if parsed.netloc not in allowed_hosts:
            raise ValueError("Zoho CRM api_domain is not a zohoapis host of the connection's data center")
        return "https://" + parsed.netloc + "/crm/v8"

    async def _resolve(self, call: Call, operation: str) -> tuple[Credentials | None, str, Exchange | None]:
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ReauthorizationRequiredError:
            return (
                None,
                "",
                Exchange(
                    "rejected",
                    failure=failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "Zoho authorization is revoked or expired; reconnect the connection",
                    ),
                    is_session_failure=True,
                ),
            )
        except Exception:
            return (
                None,
                "",
                Exchange(
                    "retry",
                    failure=failure(
                        FailureKind.AUTHENTICATION, operation, "Zoho credentials are temporarily unavailable"
                    ),
                    is_session_failure=True,
                ),
            )
        try:
            return credentials, self._api_base_url(credentials), None
        except ValueError as error:
            return (
                None,
                "",
                Exchange(
                    "defect",
                    failure=failure(
                        FailureKind.AUTHENTICATION,
                        operation,
                        "Zoho CRM connection credentials are missing or invalid: " + str(error),
                    ),
                    is_session_failure=True,
                ),
            )

    async def exchange(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        query: str = "",
        payload: Any = None,
        deadline: float | None = None,
    ) -> Exchange:
        """Send one request, refreshing a 401 once when the source supports it."""
        operation_deadline = deadline if deadline is not None else time.monotonic() + 25
        try:
            async with asyncio.timeout_at(operation_deadline):
                credentials, base_url, session_failure = await self._resolve(call, operation)
        except TimeoutError:
            return Exchange("retry", failure=failure(FailureKind.TRANSPORT, operation,
                "the operation deadline passed before Zoho CRM answered"), is_session_failure=True)
        if session_failure is not None:
            return session_failure
        assert credentials is not None
        try:
            encoded = (
                json.dumps(payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False).encode()
                if payload is not None
                else None
            )
        except (TypeError, ValueError):
            return Exchange(
                "defect",
                failure=failure(FailureKind.LOCAL_DEFECT, operation, "Zoho CRM request could not be built"),
            )
        for attempt in range(2):
            if time.monotonic() >= operation_deadline:
                return Exchange("retry", failure=failure(FailureKind.TRANSPORT, operation,
                    "the operation deadline passed before Zoho CRM answered"))
            token = credentials.access_token.reveal() if credentials.access_token is not None else ""
            headers = {"Authorization": "Zoho-oauthtoken " + token, "Accept": "application/json"}
            if encoded is not None:
                headers["Content-Type"] = "application/json"
            try:
                request = self.http_client.build_request(
                    method, base_url + path + ("?" + query if query else ""), headers=headers, content=encoded
                )
                async with asyncio.timeout_at(operation_deadline):
                    response, _ = await send_traced(self.http_client, request)
            except (ValueError, httpx.InvalidURL):
                return Exchange(
                    "defect",
                    failure=failure(
                        FailureKind.LOCAL_DEFECT, operation, "Zoho CRM request could not be built"
                    ),
                )
            except ProviderTransportError:
                return Exchange(
                    "retry",
                    failure=failure(
                        FailureKind.TRANSPORT, operation, "Zoho CRM request failed before a response arrived"
                    ),
                )
            except TimeoutError:
                return Exchange("retry", failure=failure(FailureKind.TRANSPORT, operation,
                    "Zoho CRM request failed before a response arrived"))
            try:
                async with asyncio.timeout_at(operation_deadline):
                    result = await self._read_response(response, operation, token)
            except TimeoutError:
                return Exchange("retry", response.status_code, response.headers,
                    failure=failure(FailureKind.TRANSPORT, operation, "Zoho CRM response could not be read"))
            finally:
                await response.aclose()
            if (
                result.status != 401
                or result.summary.code == "OAUTH_SCOPE_MISMATCH"
                or attempt
                or not isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                return result
            try:
                async with asyncio.timeout_at(operation_deadline):
                    replacement = await resolve_credential_after_rejection(
                        self.credentials, call, self.refresh_driver
                    )
                if replacement.auth_method_id != credentials.auth_method_id:
                    return result
                base_url = self._api_base_url(replacement)
            except Exception:
                return result
            credentials = replacement
        raise RuntimeError("Zoho credential resend budget exhausted")

    async def _read_response(self, response: httpx.Response, operation: str, token: str) -> Exchange:
        status = response.status_code
        headers = response.headers
        is_success = 200 <= status < 300
        credits = headers.get("X-API-CREDITS-REMAINING", "")
        credits = credits if _CREDITS.fullmatch(credits) else ""
        try:
            body = await read_bounded_body(response, self.max_response_bytes if is_success else 65536)
        except BodyTooLargeError:
            if is_success:
                return Exchange(
                    "invalid",
                    status,
                    headers,
                    failure=failure(
                        FailureKind.RESPONSE_TOO_LARGE,
                        operation,
                        "Zoho CRM response exceeds the configured maxResponseBytes limit",
                    ),
                    remaining_credits=credits,
                )
            body = b""
        except ProviderTransportError:
            return Exchange(
                "retry",
                status,
                headers,
                failure=failure(FailureKind.TRANSPORT, operation, "Zoho CRM response could not be read"),
                remaining_credits=credits,
            )
        if token.encode() in body:
            if is_success:
                return Exchange(
                    "invalid",
                    status,
                    headers,
                    failure=failure(
                        FailureKind.PROTOCOL,
                        operation,
                        "Zoho CRM response reflected the connection credential",
                    ),
                    remaining_credits=credits,
                )
            body = b""
        if is_success:
            return Exchange("success", status, headers, body=body, remaining_credits=credits)
        summary = describe_error(body)
        retry_after = parse_retry_after(headers.get("Retry-After", ""), datetime.now(UTC))
        if status == 429:
            return Exchange(
                "retry",
                status,
                headers,
                summary=summary,
                retry_after=retry_after,
                failure=failure(
                    FailureKind.RATE_LIMIT,
                    operation,
                    summary.describe("Zoho CRM rate limited the request (HTTP 429)"),
                ),
                remaining_credits=credits,
            )
        if status == 408 or status >= 500:
            return Exchange(
                "retry",
                status,
                headers,
                summary=summary,
                retry_after=retry_after,
                failure=failure(
                    FailureKind.AVAILABILITY,
                    operation,
                    summary.describe(f"Zoho CRM could not complete the request (HTTP {status})"),
                ),
                remaining_credits=credits,
            )
        if 300 <= status < 400:
            return Exchange(
                "rejected",
                status,
                headers,
                summary=summary,
                failure=failure(
                    FailureKind.PROTOCOL,
                    operation,
                    f"Zoho CRM redirected the request (HTTP {status}); check that the connection uses the account's data center",
                ),
                remaining_credits=credits,
            )
        return classify_rejection(status, headers, summary, operation, credits)

    def receipt(self, call: Call, exchange: Exchange, record_id: str = "") -> Receipt:
        """Record the safe provider correlation and API credit count."""
        if exchange.is_session_failure:
            return Receipt()
        metadata = {"remainingCredits": exchange.remaining_credits} if exchange.remaining_credits else {}
        return Receipt(
            call.id, "zoho-crm", datetime.now(UTC), call.idempotency_key, record_id, metadata=metadata
        )


def classify_rejection(
    status: int, headers: httpx.Headers, summary: ErrorSummary, operation: str, credits: str
) -> Exchange:
    """Map conclusive request and record errors to failure kinds."""
    code = summary.code
    if code == "OAUTH_SCOPE_MISMATCH":
        kind, message = (
            FailureKind.AUTHORIZATION,
            f"Zoho CRM refused the request (HTTP {status}): the token lacks {_SCOPES[operation]}; reconnect and accept every requested scope",
        )
    elif status == 401:
        kind, message = (
            FailureKind.AUTHENTICATION,
            "Zoho CRM rejected the access token (HTTP 401); reconnect the connection",
        )
    elif status == 403 or code in _PERMISSION_CODES:
        kind, message = (
            FailureKind.AUTHORIZATION,
            f"Zoho CRM refused the request (HTTP {status}): the connection's user lacks permission",
        )
    elif code == "DUPLICATE_DATA":
        kind, message = (
            FailureKind.CONFLICT,
            f"Zoho CRM found another record with a value of a unique field (HTTP {status})",
        )
    elif summary.record_id_invalid():
        kind, message = FailureKind.NOT_FOUND, f"Zoho CRM found no record with the ID (HTTP {status})"
    elif status == 404:
        kind, message = FailureKind.NOT_FOUND, "Zoho CRM found no such resource (HTTP 404)"
    elif status in (400, 422) or 200 <= status < 300:
        kind, message = FailureKind.VALIDATION, f"Zoho CRM rejected the request (HTTP {status})"
    else:
        kind, message = FailureKind.PROVIDER_REJECTION, f"Zoho CRM rejected the request (HTTP {status})"
    return Exchange(
        "rejected",
        status,
        headers,
        summary=summary,
        failure=failure(kind, operation, summary.describe(message)),
        remaining_credits=credits,
    )


def path_escape(value: str) -> str:
    """Escape a path segment as Go url.PathEscape does."""
    return quote(value, safe="$&+-._~:=@")


def query_escape(value: str) -> str:
    """Escape a query value as Go url.QueryEscape does."""
    return quote(value, safe="-._~").replace("%20", "+")
