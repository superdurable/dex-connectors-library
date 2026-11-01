# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Unsplash photo reads and Go-compatible branch classification."""

import json
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime, timedelta
from typing import Any, TypeVar
from urllib.parse import quote, urlencode, urlsplit

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    OperationRef,
    QueryAttempt,
    QueryDefinition,
    ReauthorizationRequiredError,
    Receipt,
    SecretString,
    StepDefaults,
)
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
from dex_connectors.sdk.retry_policy import default_connector_step_execute_retry_policy

from .models import GetPhotoInput, Photo, PhotoPage, SearchPhotosInput

CONNECTOR_ID = "unsplash"
SEARCH_PHOTOS_OPERATION_ID = "searchPhotos"
GET_PHOTO_OPERATION_ID = "getPhoto"
SEARCHED = "searched"
FOUND = "found"
NOT_FOUND = "notFound"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.unsplash.com"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_INVALID_RESPONSE_BRANCH = BranchDefinition(
    INVALID_RESPONSE, "Unsplash returned malformed or oversized data.", True
)
_DEFECT_BRANCH = BranchDefinition(DEFECT, "Local input or credentials are invalid.", True)
SEARCH_PHOTOS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, SEARCH_PHOTOS_OPERATION_ID),
    (
        BranchDefinition(SEARCHED, "Unsplash returned a page of photos, possibly empty."),
        BranchDefinition(PROVIDER_REJECTED, "Unsplash rejected the search or key.", True),
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
GET_PHOTO_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, GET_PHOTO_OPERATION_ID),
    (
        BranchDefinition(FOUND, "Unsplash returned the photo."),
        BranchDefinition(NOT_FOUND, "No photo has that ID.", True),
        BranchDefinition(PROVIDER_REJECTED, "Unsplash rejected the request or key.", True),
        _INVALID_RESPONSE_BRANCH,
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class Config:
    """Unsplash HTTP settings; zero or blank values use manifest defaults."""

    base_url: str = _DEFAULT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Unsplash application access key, resolved anew for every provider attempt."""

    access_key: SecretString


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Unsplash configuration is required")
        if credentials is None:
            raise ValueError("Unsplash credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Unsplash timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def search_photos(self) -> "SearchPhotosOperation":
        """Bind the photo search Query to this client."""
        return SearchPhotosOperation(self)

    def get_photo(self) -> "GetPhotoOperation":
        """Bind the photo lookup Query to this client."""
        return GetPhotoOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def get(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        not_found: str,
        rejection_message: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go get helper and its callers."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        key = "" if credentials is None else credentials.access_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(
                operation,
                DEFECT,
                empty,
                FailureKind.AUTHENTICATION,
                "Unsplash credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Authorization": "Client-ID " + key, "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _failed(
                operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "Unsplash request could not be built"
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.TRANSPORT, "Unsplash response is unavailable", 0)
        try:
            status = response.status_code
            if status in (408, 429) or status >= 500:
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                return _retry(
                    operation, FailureKind.AVAILABILITY, "Unsplash is temporarily unavailable", retry_after
                )
            if status == 404 and not_found:
                return _failed(
                    operation, not_found, empty, FailureKind.NOT_FOUND, "Unsplash photo was not found"
                )
            if status != 200:
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    _rejection_kind(status),
                    f"{rejection_message} {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Unsplash response exceeded the limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "Unsplash response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except _InvalidPhotoError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "Unsplash returned an invalid photo",
                )
            except (ValueError, TypeError, UnicodeDecodeError):
                message = (
                    "Unsplash returned an invalid search page"
                    if operation == SEARCH_PHOTOS_OPERATION_ID
                    else "Unsplash returned malformed photo JSON"
                )
                return _failed(operation, INVALID_RESPONSE, empty, FailureKind.PROTOCOL, message)
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class SearchPhotosOperation:
    """Search public Unsplash photos by phrase."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Unsplash client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return SEARCH_PHOTOS_DEFINITION

    async def invoke(self, call: Call, input: SearchPhotosInput) -> QueryAttempt[PhotoPage]:
        """Validate the search, read once, then select a branch or safe retry."""
        operation = SEARCH_PHOTOS_OPERATION_ID
        if (
            not isinstance(input, SearchPhotosInput)
            or not isinstance(input.query, str)
            or type(input.page) is not int
            or type(input.per_page) is not int
            or not input.query.strip()
            or input.page < 0
            or not 0 <= input.per_page <= 30
        ):
            return _failed(
                operation, DEFECT, PhotoPage(), FailureKind.VALIDATION, "query and page size must be valid"
            )
        parameters = {"query": input.query}
        if input.page > 0:
            parameters["page"] = str(input.page)
        if input.per_page > 0:
            parameters["per_page"] = str(input.per_page)
        return await self.client.get(
            call,
            operation,
            "/search/photos?" + urlencode(sorted(parameters.items())),
            PhotoPage(),
            SEARCHED,
            "",
            "Unsplash rejected search with HTTP",
            _decode_page,
        )


class GetPhotoOperation:
    """Read one Unsplash photo by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Unsplash client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_PHOTO_DEFINITION

    async def invoke(self, call: Call, input: GetPhotoInput) -> QueryAttempt[Photo]:
        """Validate the ID, read once, then select a branch or safe retry."""
        operation = GET_PHOTO_OPERATION_ID
        if (
            not isinstance(input, GetPhotoInput)
            or not isinstance(input.id, str)
            or not input.id.strip()
            or any(character in input.id for character in "/?&#")
        ):
            return _failed(operation, DEFECT, Photo(), FailureKind.VALIDATION, "photo ID is invalid")
        return await self.client.get(
            call,
            operation,
            "/photos/" + quote(input.id, safe=""),
            Photo(),
            FOUND,
            NOT_FOUND,
            "Unsplash rejected photo lookup with HTTP",
            _decode_photo,
        )


class _InvalidPhotoError(ValueError):
    """A well-formed photo object lacked required identity, attribution, or HTTPS links."""


def _decode_page(document: Any) -> PhotoPage:
    document = _object(document)
    total = _integer(document, "total")
    total_pages = _integer(document, "total_pages")
    results = document.get("results")
    if total < 0 or total_pages < 0 or not isinstance(results, list):
        raise ValueError
    photos = []
    for item in results:
        if not isinstance(item, dict):
            raise ValueError
        photos.append(_decode_photo(item))
    return PhotoPage(total, total_pages, tuple(photos))


def _decode_photo(document: Any) -> Photo:
    document = _object(document)
    user = _object(document.get("user") or {})
    photo = Photo(
        id=_string(document, "id"),
        html_url=_string(_object(document.get("links") or {}), "html"),
        regular_url=_string(_object(document.get("urls") or {}), "regular"),
        photographer_name=_string(user, "name"),
        photographer_url=_string(_object(user.get("links") or {}), "html"),
        description=_optional_string(document, "description"),
        alt_description=_optional_string(document, "alt_description"),
    )
    if (
        not photo.id
        or not photo.photographer_name
        or not all(_is_https(url) for url in (photo.html_url, photo.regular_url, photo.photographer_url))
    ):
        raise _InvalidPhotoError
    return photo


def _is_https(value: str) -> bool:
    parsed = urlsplit(value)
    return parsed.scheme == "https" and bool(parsed.netloc)


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


def _integer(document: dict[str, Any], name: str) -> int:
    value = document.get(name)
    if value is None:
        return 0
    if type(value) is not int:
        raise ValueError
    return value


def _string(document: dict[str, Any], name: str) -> str:
    value = _optional_string(document, name)
    return "" if value is None else value


def _optional_string(document: dict[str, Any], name: str) -> str | None:
    value = document.get(name)
    if value is not None and not isinstance(value, str):
        raise ValueError
    return value
