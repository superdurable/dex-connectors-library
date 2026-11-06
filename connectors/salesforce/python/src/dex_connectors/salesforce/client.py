# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Salesforce REST client with Go-compatible response classification."""

from __future__ import annotations

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import TYPE_CHECKING
from urllib.parse import quote, urlencode, urlsplit

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
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config, Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS
from .models import ProviderError

if TYPE_CHECKING:
    from .operations import (
        GetRecordOperation,
        QueryRecordsOperation,
        UpdateRecordOperation,
        UpsertRecordByExternalIDOperation,
    )

_API_VERSION = re.compile(r"v([1-9][0-9]{1,2})\.0\Z")
_ERROR_CODE = re.compile(r"[A-Z][A-Z0-9_]{0,79}\Z")
_API_USAGE = re.compile(r"(?:^|,)\s*api-usage=([0-9]{1,12}/[0-9]{1,12})\s*(?:,|$)")
_API_NAME = re.compile(r"[A-Za-z][A-Za-z0-9_]*\Z")
_ACCESS_CODES = frozenset(
    {
        "API_CURRENTLY_DISABLED",
        "API_DISABLED_FOR_ORG",
        "FUNCTIONALITY_NOT_ENABLED",
        "INSUFFICIENT_ACCESS",
        "INSUFFICIENT_ACCESS_OR_READONLY",
        "INVALID_AUTH_HEADER",
        "INVALID_SESSION_ID",
        "NOT_API_ENABLED",
        "ORG_LOCKED",
    }
)


def go_path_escape(value: str) -> str:
    """Escape one path segment using Go url.PathEscape's literal character set."""
    return quote(value, safe="-._~$&+=:@")


def go_query(values: dict[str, str]) -> str:
    """Encode sorted query keys with Go url.Values.Encode spacing and escaping."""
    return urlencode(sorted(values.items()), safe="~")


def is_api_name(value: str) -> bool:
    """Validate a Salesforce object or field API name."""
    return len(value.encode()) <= 255 and bool(_API_NAME.fullmatch(value)) and not value.endswith("_")


def is_record_id(value: str) -> bool:
    """Validate either Salesforce record ID wire form."""
    return bool(re.fullmatch(r"[A-Za-z0-9]{15}(?:[A-Za-z0-9]{3})?", value))


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Construct a safe failure without provider error text or secrets."""
    return Failure(kind, "salesforce", operation, message)


@dataclass(frozen=True, slots=True)
class Response:
    """Bounded response and safe metadata from one Salesforce request."""

    status: int
    headers: httpx.Headers
    body: bytes
    api_usage: str


@dataclass(frozen=True, slots=True)
class Outcome:
    """Classified failure route and optional validated provider codes."""

    route: str
    failure: Failure
    retry_after: float = 0
    provider_errors: tuple[ProviderError, ...] = ()
    api_usage: str = ""


class Client:
    """Run Salesforce REST operations with one optional 401 refresh and bounded reads."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Salesforce configuration and credential provider are required")
        match = _API_VERSION.fullmatch(config.api_version)
        if not match or int(match[1]) < 46:
            raise ValueError("Salesforce apiVersion must be a vNN.0 version no older than v46.0")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Salesforce maxResponseBytes must be positive")
        if type(config.query_batch_size) is not int or not 200 <= config.query_batch_size <= 2000:
            raise ValueError("Salesforce queryBatchSize must be from 200 to 2000")
        self.api_version = config.api_version
        self.max_response_bytes = config.max_response_bytes
        self.query_batch_size = config.query_batch_size
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self._owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Salesforce", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client, now=self.now
        )

    async def aclose(self) -> None:
        """Close only an HTTP client this connector constructed."""
        if self._owns_http_client:
            await self.http_client.aclose()

    def query_records(self) -> QueryRecordsOperation:
        """Bind the queryRecords operation."""
        from .operations import QueryRecordsOperation

        return QueryRecordsOperation(self)

    def get_record(self) -> GetRecordOperation:
        """Bind the getRecord operation."""
        from .operations import GetRecordOperation

        return GetRecordOperation(self)

    def upsert_record_by_external_id(self) -> UpsertRecordByExternalIDOperation:
        """Bind the upsertRecordByExternalId operation."""
        from .operations import UpsertRecordByExternalIDOperation

        return UpsertRecordByExternalIDOperation(self)

    def update_record(self) -> UpdateRecordOperation:
        """Bind the updateRecord operation."""
        from .operations import UpdateRecordOperation

        return UpdateRecordOperation(self)

    def data_path(self, *segments: str) -> str:
        """Build a versioned REST path using Go's per-segment escaping."""
        return "/" + "/".join(
            ("services", "data", self.api_version, *(go_path_escape(item) for item in segments))
        )

    def receipt(self, call: Call, object_id: str = "", api_usage: str = "") -> Receipt:
        """Record call identity, Salesforce object identity, and validated API usage."""
        return Receipt(
            call.id,
            "salesforce",
            self.now().astimezone(UTC),
            call.idempotency_key,
            object_id,
            metadata={"apiUsage": api_usage} if api_usage else {},
        )

    async def resolve_credentials(
        self, call: Call, operation: str
    ) -> tuple[Credentials | None, Outcome | None]:
        """Resolve a grant and distinguish terminal from temporary failures."""
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ReauthorizationRequiredError:
            return None, Outcome(
                "providerRejected",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Salesforce authorization is revoked or expired; reauthorize the connection",
                ),
            )
        except Exception:
            return None, Outcome(
                "retry",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Salesforce credentials are temporarily unavailable",
                ),
            )
        if not self._valid_credentials(credentials):
            return None, Outcome(
                "defect",
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    "Salesforce connection credentials are missing or invalid",
                ),
            )
        return credentials, None

    @staticmethod
    def _valid_credentials(credentials: Credentials) -> bool:
        if credentials.access_token is None or not is_header_safe_credential(
            credentials.access_token.reveal()
        ):
            return False
        try:
            instance_url = validate_base_url(credentials.instance_url)
            if urlsplit(instance_url).path not in ("", "/"):
                return False
        except ValueError:
            return False
        return credentials.auth_method_id in (
            "",
            "salesforce-oauth",
            "salesforce-sandbox-oauth",
            "salesforce-jwt-bearer",
        )

    async def send(
        self,
        call: Call,
        credentials: Credentials,
        operation: str,
        method: str,
        path: str,
        *,
        query: dict[str, str] | None = None,
        body: bytes | None = None,
        response_limit: int | None = None,
        query_batch_size: int | None = None,
        duplicate_retry: bool = False,
        oversized_route: str = "invalidResponse",
    ) -> tuple[Response | None, Outcome | None]:
        """Send one REST request and classify a failed or unreadable exchange."""
        limit = response_limit if response_limit is not None else self.max_response_bytes
        for attempt in range(2):
            try:
                instance = validate_base_url(credentials.instance_url)
                if urlsplit(instance).path not in ("", "/"):
                    raise ValueError("instance URL has path")
                target = instance + path + ("?" + go_query(query) if query else "")
                access_token = credentials.access_token
                if access_token is None:
                    raise ValueError("access token is missing")
                headers = {
                    "Authorization": "Bearer " + access_token.reveal(),
                    "Accept": "application/json",
                }
                if body is not None:
                    headers["Content-Type"] = "application/json"
                if query_batch_size is not None:
                    headers["Sforce-Query-Options"] = "batchSize=" + str(query_batch_size)
                request = self.http_client.build_request(method, target, headers=headers, content=body)
            except (ValueError, httpx.InvalidURL):
                return None, Outcome(
                    "defect",
                    failure(operation, FailureKind.LOCAL_DEFECT, "Salesforce request URL could not be built"),
                )
            try:
                http_response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError:
                return None, Outcome(
                    "retry",
                    failure(operation, FailureKind.TRANSPORT, "Salesforce request outcome is unknown"),
                )
            status, response_headers = http_response.status_code, http_response.headers
            api_usage = parse_api_usage(response_headers.get("Sforce-Limit-Info", ""))
            try:
                response_body = await read_bounded_body(http_response, max(limit, MAX_ERROR_BODY_BYTES))
            except BodyTooLargeError:
                if 200 <= status < 300:
                    return None, Outcome(
                        oversized_route,
                        failure(
                            operation,
                            FailureKind.RESPONSE_TOO_LARGE,
                            "Salesforce response exceeds the configured maxResponseBytes limit",
                        ),
                        api_usage=api_usage,
                    )
                response_body = b""
            except ProviderTransportError:
                return None, Outcome(
                    "retry",
                    failure(operation, FailureKind.TRANSPORT, "Salesforce request outcome is unknown"),
                )
            if 200 <= status < 300 and len(response_body) > limit:
                return None, Outcome(
                    oversized_route,
                    failure(
                        operation,
                        FailureKind.RESPONSE_TOO_LARGE,
                        "Salesforce response exceeds the configured maxResponseBytes limit",
                    ),
                    api_usage=api_usage,
                )
            response = Response(status, response_headers, response_body, api_usage)
            if (
                status == 401
                and attempt == 0
                and isinstance(self.credentials, RejectedCredentialRefreshingProvider)
            ):
                try:
                    replacement = await resolve_credential_after_rejection(
                        self.credentials, call, self.refresh_driver
                    )
                    if self._valid_credentials(replacement):
                        credentials = replacement
                        continue
                except Exception:
                    pass
            if 200 <= status < 300 or status == 300:
                return response, None
            return response, self.classify(operation, response, duplicate_retry)
        return None, Outcome(
            "defect",
            failure(
                operation, FailureKind.LOCAL_DEFECT, "authenticated Salesforce request retry was exhausted"
            ),
        )

    def classify(self, operation: str, response: Response, duplicate_retry: bool) -> Outcome:
        """Classify Salesforce status and validated error codes without retaining messages."""
        provider_errors = parse_provider_errors(response.body)
        code = provider_errors[0].error_code if provider_errors else ""
        status = response.status
        retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())

        def named(message: str) -> str:
            return message + (" (" + code + ")" if code else "")

        if code == "REQUEST_LIMIT_EXCEEDED" or status == 429:
            return Outcome(
                "retry",
                failure(operation, FailureKind.RATE_LIMIT, named("Salesforce API request limit was reached")),
                retry_after,
                provider_errors,
                response.api_usage,
            )
        if code == "UNABLE_TO_LOCK_ROW":
            return Outcome(
                "retry",
                failure(operation, FailureKind.CONFLICT, named("Salesforce could not lock the record")),
                retry_after,
                provider_errors,
                response.api_usage,
            )
        if code == "DUPLICATE_VALUE" and duplicate_retry:
            return Outcome(
                "retry",
                failure(operation, FailureKind.CONFLICT, named("a concurrent write holds the unique value")),
                0,
                provider_errors,
                response.api_usage,
            )
        if code == "SERVER_UNAVAILABLE" or status >= 500:
            return Outcome(
                "retry",
                failure(operation, FailureKind.AVAILABILITY, named("Salesforce is temporarily unavailable")),
                retry_after,
                provider_errors,
                response.api_usage,
            )
        if status == 401 or code == "INVALID_SESSION_ID":
            return Outcome(
                "providerRejected",
                failure(operation, FailureKind.AUTHENTICATION, named("Salesforce rejected the session")),
                provider_errors=provider_errors,
                api_usage=response.api_usage,
            )
        if code in _ACCESS_CODES or status == 403:
            return Outcome(
                "providerRejected",
                failure(operation, FailureKind.AUTHORIZATION, named("Salesforce denied access")),
                provider_errors=provider_errors,
                api_usage=response.api_usage,
            )
        if code in ("NOT_FOUND", "ENTITY_IS_DELETED") or status == 404:
            return Outcome(
                "notFound",
                failure(operation, FailureKind.NOT_FOUND, named("Salesforce did not find the resource")),
                provider_errors=provider_errors,
                api_usage=response.api_usage,
            )
        if status in (400, 409, 412, 422):
            return Outcome(
                "requestRejected",
                failure(operation, FailureKind.VALIDATION, named("Salesforce rejected the request")),
                provider_errors=provider_errors,
                api_usage=response.api_usage,
            )
        return Outcome(
            "providerRejected",
            failure(operation, FailureKind.PROVIDER_REJECTION, named("Salesforce rejected the request")),
            provider_errors=provider_errors,
            api_usage=response.api_usage,
        )


def parse_provider_errors(body: bytes) -> tuple[ProviderError, ...]:
    """Keep only valid Salesforce error codes and field API names."""
    try:
        values = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return ()
    if not isinstance(values, list):
        return ()
    errors: list[ProviderError] = []
    for value in values:
        if not isinstance(value, dict) or not isinstance(value.get("errorCode"), str):
            continue
        code = value["errorCode"]
        if not _ERROR_CODE.fullmatch(code):
            continue
        fields = value.get("fields")
        names = (
            tuple(item for item in fields if isinstance(item, str) and is_api_name(item))
            if isinstance(fields, list)
            else ()
        )
        errors.append(ProviderError(code, names))
    return tuple(errors)


def parse_api_usage(value: str) -> str:
    """Extract only the bounded numeric api-usage ratio."""
    match = _API_USAGE.search(value)
    return match[1] if match else ""


__all__ = [
    "Client",
    "Config",
    "Credentials",
    "Outcome",
    "Response",
    "failure",
    "go_path_escape",
    "go_query",
    "is_api_name",
    "is_record_id",
    "parse_provider_errors",
]
