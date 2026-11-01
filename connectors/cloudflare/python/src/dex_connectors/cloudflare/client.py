# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Cloudflare zone listing and URL cache purges with Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlsplit

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
)
from dex_connectors.sdk import OperationRef as OperationRef
from dex_connectors.sdk import SecretString as SecretString
from dex_connectors.sdk import StepDefaults as StepDefaults
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
from dex_connectors.sdk.retry_policy import (
    default_connector_step_execute_retry_policy as default_connector_step_execute_retry_policy,
)

from ._generated.config import (
    Config as Config,
)
from ._generated.config import (
    Credentials as Credentials,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_ZONES_DEFINITION as LIST_ZONES_DEFINITION,
)
from ._generated.definitions import (
    LIST_ZONES_OPERATION_ID as LIST_ZONES_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from ._generated.definitions import (
    PURGE_CACHE_BY_URLS_DEFINITION as PURGE_CACHE_BY_URLS_DEFINITION,
)
from ._generated.definitions import (
    PURGE_CACHE_BY_URLS_OPERATION_ID as PURGE_CACHE_BY_URLS_OPERATION_ID,
)
from ._generated.definitions import (
    PURGED as PURGED,
)
from .models import CachePurge, ListZonesInput, PurgeCacheByURLsInput, PurgeRequest, Zone, ZonesPage

_DEFAULT_BASE_URL = "https://api.cloudflare.com/client/v4"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_PURGE_URLS = 30
_PATH_SEGMENT_SAFE = "$&+:=@"
_STATUS_KINDS = {
    401: FailureKind.AUTHENTICATION,
    403: FailureKind.AUTHORIZATION,
    404: FailureKind.NOT_FOUND,
    400: FailureKind.VALIDATION,
    422: FailureKind.VALIDATION,
}

Output = TypeVar("Output")






@dataclass(frozen=True, slots=True)
class _Outcome:
    """A terminal or retry attempt whose empty value the calling operation supplies."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def as_query(self, empty: Output) -> QueryAttempt[Output]:
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)

    def as_mutation(self, empty: Output) -> MutationAttempt[Output]:
        if self.is_retry:
            return MutationAttempt.retry(self.failure, self.retry_after_seconds)
        return MutationAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


@dataclass(frozen=True, slots=True)
class _Success:
    """A decoded happy-branch value."""

    value: Any


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Cloudflare configuration is required")
        if credentials is None:
            raise ValueError("Cloudflare credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Cloudflare timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_zones(self) -> "ListZonesOperation":
        """Bind the listZones Query to this client."""
        return ListZonesOperation(self)

    def purge_cache_by_urls(self) -> "PurgeCacheByURLsOperation":
        """Bind the purgeCacheByURLs Mutation to this client."""
        return PurgeCacheByURLsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def execute(
        self,
        call: Call,
        operation: str,
        method: str,
        path: str,
        body: bytes | None,
        has_not_found: bool,
        decode: Callable[[Any], Any],
    ) -> _Success | _Outcome:
        """Send one request and classify it exactly like the Go executeQuery and executeMutation helpers."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        token = credentials.api_token.reveal()
        if not token or not is_header_safe_credential(token):
            return _outcome(
                DEFECT, operation, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {"Authorization": "Bearer " + token, "Accept": "application/json"}
        if body is not None:
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(
                method, self.base_url + path, content=body, headers=headers
            )
        except (httpx.HTTPError, ValueError):
            return _outcome(DEFECT, operation, FailureKind.LOCAL_DEFECT, "request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _outcome(
                "", operation, FailureKind.AVAILABILITY, "Cloudflare request failed", is_retry=True
            )
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                kind = FailureKind.RATE_LIMIT if status == 429 else FailureKind.AVAILABILITY
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _outcome("", operation, kind, f"Cloudflare returned HTTP {status}", True, retry_after)
            if status != 200:
                branch = NOT_FOUND if status == 404 and has_not_found else PROVIDER_REJECTED
                kind = _STATUS_KINDS.get(status, FailureKind.PROVIDER_REJECTION)
                return _outcome(branch, operation, kind, f"Cloudflare returned HTTP {status}")
            try:
                response_body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _outcome(
                    INVALID_RESPONSE,
                    operation,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _outcome(
                    "", operation, FailureKind.AVAILABILITY, "response was interrupted", is_retry=True
                )
            try:
                document = json.loads(response_body)
            except (ValueError, UnicodeDecodeError):
                document = None
                is_valid_json = False
            else:
                is_valid_json = True
            if is_valid_json and isinstance(document, dict) and document.get("success") is False:
                return _outcome(
                    PROVIDER_REJECTED,
                    operation,
                    FailureKind.PROVIDER_REJECTION,
                    "Cloudflare rejected the request",
                )
            try:
                if not is_valid_json:
                    raise ValueError
                return _Success(decode(document))
            except (ValueError, TypeError):
                return _outcome(INVALID_RESPONSE, operation, FailureKind.PROTOCOL, "response was malformed")
        finally:
            await response.aclose()


class ListZonesOperation:
    """List the first page of Cloudflare zones."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Cloudflare client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_ZONES_DEFINITION

    async def invoke(self, call: Call, input: ListZonesInput) -> QueryAttempt[ZonesPage]:
        """Read the first zone page once, then select a branch or safe retry."""
        result = await self.client.execute(
            call, LIST_ZONES_OPERATION_ID, "GET", "/zones", None, False, _decode_zones
        )
        if isinstance(result, _Outcome):
            return result.as_query(ZonesPage())
        return QueryAttempt.branch_result(LISTED, result.value, Receipt())


class PurgeCacheByURLsOperation:
    """Purge specific URLs from one zone; repeating the same purge has the same effect."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Cloudflare client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return PURGE_CACHE_BY_URLS_DEFINITION

    def idempotency_key(self, call_id: CallID, input: PurgeCacheByURLsInput) -> IdempotencyKey:
        """Identify the Dex call; purging the same URLs is repeat safe."""
        return IdempotencyKey(str(call_id))

    async def invoke(self, call: Call, input: PurgeCacheByURLsInput) -> MutationAttempt[CachePurge]:
        """Validate, purge once, then select a branch or safe retry."""
        operation = PURGE_CACHE_BY_URLS_OPERATION_ID
        message = _purge_validation_message(input)
        if message:
            return MutationAttempt.branch_result(
                DEFECT,
                CachePurge(),
                Receipt(),
                Failure(FailureKind.VALIDATION, CONNECTOR_ID, operation, message),
            )
        body = json.dumps({"files": list(input.urls)}, separators=(",", ":")).encode()
        path = "/zones/" + quote(input.zone_id, safe=_PATH_SEGMENT_SAFE) + "/purge_cache"
        result = await self.client.execute(call, operation, "POST", path, body, True, _decode_cache_purge)
        if isinstance(result, _Outcome):
            return result.as_mutation(CachePurge())
        return MutationAttempt.branch_result(PURGED, result.value, Receipt())


def _purge_validation_message(input: PurgeCacheByURLsInput) -> str:
    if (
        not isinstance(input, PurgeCacheByURLsInput)
        or not isinstance(input.zone_id, str)
        or not input.zone_id.strip()
        or not 1 <= len(input.urls) <= _MAX_PURGE_URLS
    ):
        return "zoneId and 1 to 30 URLs are required"
    if not all(_is_absolute_http_url(raw_url) for raw_url in input.urls):
        return "each URL must be an absolute HTTP or HTTPS URL"
    return ""


def _is_absolute_http_url(raw_url: object) -> bool:
    if not isinstance(raw_url, str) or any(
        ord(character) < 0x20 or character == "\x7f" for character in raw_url
    ):
        return False
    try:
        parsed = urlsplit(raw_url)
    except ValueError:
        return False
    return (
        parsed.scheme in ("http", "https")
        and bool(parsed.netloc)
        and "@" not in parsed.netloc
        and " " not in parsed.netloc
    )


def _decode_zones(document: Any) -> ZonesPage:
    if not isinstance(document, dict):
        raise ValueError
    result = document.get("result")
    if _optional(document, "success", False) is not True or not isinstance(result, list):
        raise ValueError
    return ZonesPage(True, tuple(_decode_zone(zone) for zone in result))


def _decode_zone(document: Any) -> Zone:
    if document is None:
        return Zone()
    if not isinstance(document, dict):
        raise ValueError
    fields = tuple(_optional(document, name, "") for name in ("id", "name", "status"))
    if not all(isinstance(value, str) for value in fields):
        raise ValueError
    return Zone(*fields)


def _decode_cache_purge(document: Any) -> CachePurge:
    if not isinstance(document, dict) or _optional(document, "success", False) is not True:
        raise ValueError
    result = _optional(document, "result", {})
    if not isinstance(result, dict):
        raise ValueError
    purge_id = _optional(result, "id", "")
    if not isinstance(purge_id, str):
        raise ValueError
    return CachePurge(True, PurgeRequest(purge_id))


def _optional(document: dict[str, Any], name: str, default: Any) -> Any:
    """Return a JSON field, treating absent or null like Go's zero value."""
    value = document.get(name)
    return default if value is None else value


def _outcome(
    branch: str,
    operation: str,
    kind: FailureKind,
    message: str,
    is_retry: bool = False,
    retry_after_seconds: float = 0,
) -> _Outcome:
    return _Outcome(branch, Failure(kind, CONNECTOR_ID, operation, message), is_retry, retry_after_seconds)
