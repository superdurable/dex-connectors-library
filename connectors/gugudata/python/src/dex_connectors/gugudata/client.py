# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""GuGuData text segmentation transport and Go-compatible Query decisions."""

import json
from datetime import UTC, datetime
from typing import Any

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
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import SEGMENT_TEXT_DEFINITION
from .models import SegmentTextInput, SegmentTextOutput

_ENDPOINT = "https://api.gugudata.com/text/segment"


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[SegmentTextOutput]:
    return QueryAttempt.branch_result(
        branch, SegmentTextOutput(), Receipt(), Failure(kind, "gugudata", "segmentText", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[SegmentTextOutput]:
    return QueryAttempt.retry(Failure(kind, "gugudata", "segmentText", message), delay)


def _valid_app_key(value: str) -> bool:
    return bool(value) and len(value) <= 1024 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own the redirect-safe HTTP transport and resolve credentials for every segmentation."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("GuGuData configuration and credential source are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("GuGuData timeout or response limit is invalid")
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def segment_text(self) -> "SegmentTextOperation":
        """Bind the text segmentation Query to this client."""
        return SegmentTextOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class SegmentTextOperation:
    """Split short text into provider-defined segments in source order."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("GuGuData client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated segmentation Query identity."""
        return SEGMENT_TEXT_DEFINITION

    async def invoke(self, call: Call, input: SegmentTextInput) -> QueryAttempt[SegmentTextOutput]:
        """Make one segmentation request and classify its provider outcome."""
        try:
            text_units = len(input.text.encode("utf-16-le")) // 2
        except UnicodeEncodeError:
            text_units = 2001
        if not input.text.strip() or text_units > 2000:
            return _branch(
                "defect",
                FailureKind.VALIDATION,
                "GuGuData requires nonblank text of at most 2000 UTF-16 units",
            )
        try:
            credentials = await self.client.credentials.resolve(call)
            app_key = credentials.app_key.reveal()
            if not _valid_app_key(app_key):
                raise ValueError("invalid AppKey")
        except (ValueError, LookupError, OSError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "GuGuData credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                _ENDPOINT,
                params={"text": input.text},
                headers={"Accept": "application/json", "X-GUGUDATA-APPKEY": app_key},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "GuGuData segmentation request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "GuGuData segmentation request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "GuGuData rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY,
                    "GuGuData segmentation service is temporarily unavailable",
                    delay,
                )
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                return _branch("providerRejected", kind, f"GuGuData rejected segmentation with HTTP {status}")
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "GuGuData response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "GuGuData response was interrupted")
        finally:
            await response.aclose()
        return _decode_segmentation(body)


def _decode_segmentation(body: bytes) -> QueryAttempt[SegmentTextOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GuGuData returned invalid segmentation data")
    if not isinstance(document, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GuGuData returned invalid segmentation data")
    status = document.get("DataStatus")
    if not isinstance(status, dict) or type(status.get("StatusCode")) is not int:
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GuGuData returned invalid segmentation data")
    code = status["StatusCode"]
    if code != 100:
        return _branch(
            "providerRejected",
            FailureKind.PROVIDER_REJECTION,
            f"GuGuData rejected segmentation with code {code}",
        )
    data = document.get("Data")
    if not isinstance(data, dict) or not isinstance(data.get("SegmentaryText"), str):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "GuGuData returned no segmentation data")
    return QueryAttempt.branch_result(
        "segmented", SegmentTextOutput(segmented_text=data["SegmentaryText"]), Receipt()
    )
