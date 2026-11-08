# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Meta Marketing API ad lookup with Go-compatible provider decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import urlencode

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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_AD_DEFINITION
from .models import AdMetadata, GetAdInput

Output = TypeVar("Output")
_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A classified provider branch or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply this decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _decision(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "meta-ads-insights", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _decision(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
    ).with_empty(empty)


def _numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


class Client:
    """Resolve a User token per call and read bounded Marketing API responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Meta Ads Insights configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Meta response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Meta timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def get_ad(self) -> "GetAdOperation":
        """Bind the ad Query."""
        return GetAdOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str) -> Any:
        """Fetch JSON or classify a provider failure without exposing its body."""
        try:
            credential = await self.credentials.resolve(call)
            access_token = credential.user_access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid User token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _decision(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + access_token,
                    "User-Agent": "Dex Connector (https://superdurable.io)",
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "meta-ads-insights", operation, "Meta request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.RATE_LIMIT,
                        "meta-ads-insights",
                        operation,
                        "Meta rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "meta-ads-insights",
                        operation,
                        "Meta is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404:
                return _decision(
                    operation, "notFound", FailureKind.NOT_FOUND, "Meta did not find the resource"
                )
            if status != 200:
                return _decision(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Meta rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _decision(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meta response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "meta-ads-insights",
                        operation,
                        "Meta response was interrupted",
                    ),
                    True,
                )
            if access_token.encode() in body:
                return _decision(
                    operation,
                    "defect",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are unavailable or invalid",
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _decision(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
                )
        finally:
            await response.aclose()


class GetAdOperation:
    """Read one ad's identity and delivery status from Meta Marketing API."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Meta Ads Insights client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated ad Query contract."""
        return GET_AD_DEFINITION

    async def invoke(self, call: Call, input: GetAdInput) -> QueryAttempt[AdMetadata]:
        """Classify ad lookups consistently with the Go connector."""
        empty = AdMetadata()
        if not isinstance(input, GetAdInput) or not _numeric_id(input.ad_id):
            return _decision("getAd", "defect", FailureKind.VALIDATION, "ad ID is invalid").with_empty(empty)
        query = urlencode({"fields": "id,name,status,effective_status"})
        response = await self.client.get(call, "getAd", f"/{input.ad_id}?{query}")
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        metadata = _project_ad(response, input.ad_id)
        if metadata is None:
            return _invalid("getAd", empty)
        return QueryAttempt.branch_result("found", metadata, Receipt())


def _project_ad(value: object, requested_id: str) -> AdMetadata | None:
    if not isinstance(value, dict) or value.get("id") != requested_id:
        return None
    name = value.get("name")
    status = value.get("status")
    effective_status = value.get("effective_status")
    if (
        not isinstance(name, str)
        or not name
        or not isinstance(status, str)
        or not status
        or not isinstance(effective_status, str)
        or not effective_status
    ):
        return None
    return AdMetadata(requested_id, name, status, effective_status)
