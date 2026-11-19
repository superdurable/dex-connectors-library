# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bmob class count HTTP transport and Go-compatible decisions."""

import json
import re
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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import COUNT_CLASS_OBJECTS_DEFINITION
from .models import CountClassObjectsInput, CountClassObjectsOutput

_CLASS_NAME = re.compile(r"[A-Za-z][A-Za-z0-9_]{0,127}\Z")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[CountClassObjectsOutput]:
    return QueryAttempt.branch_result(
        branch, CountClassObjectsOutput(), Receipt(), Failure(kind, "bmob", "countClassObjects", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[CountClassObjectsOutput]:
    return QueryAttempt.retry(Failure(kind, "bmob", "countClassObjects", message), delay)


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value) <= 512 and all(0x20 < ord(character) < 0x7F for character in value)


class Client:
    """Own Bmob settings and resolve credentials on each class count."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Bmob configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Bmob timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def count_class_objects(self) -> "CountClassObjectsOperation":
        """Bind the class count Query."""
        return CountClassObjectsOperation(self)

    async def aclose(self) -> None:
        """Close only an HTTP client this instance created."""
        if self._owns_client:
            await self.http_client.aclose()


class CountClassObjectsOperation:
    """Count one existing Bmob class without reading object contents."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Bmob client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated class count Query identity."""
        return COUNT_CLASS_OBJECTS_DEFINITION

    async def invoke(
        self, call: Call, input: CountClassObjectsInput
    ) -> QueryAttempt[CountClassObjectsOutput]:
        """Request the count and classify the observed provider outcome."""
        class_name = input.class_name if isinstance(input, CountClassObjectsInput) else ""
        if _CLASS_NAME.fullmatch(class_name) is None:
            return _branch("defect", FailureKind.VALIDATION, "class name is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            application_id = credentials.application_id.reveal()
            rest_api_key = credentials.rest_api_key.reveal()
            if not _valid_credential(application_id) or not _valid_credential(rest_api_key):
                raise ValueError("invalid Bmob credential")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                f"{self.client.base_url}/1/classes/{class_name}",
                params={"count": "1", "limit": "0"},
                headers={
                    "Accept": "application/json",
                    "X-Bmob-Application-Id": application_id,
                    "X-Bmob-REST-API-Key": rest_api_key,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "class count request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "Bmob request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Bmob rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Bmob is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                    "Bmob response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Bmob response was interrupted")
            if status != 200:
                kind = FailureKind.PROVIDER_REJECTION
                if status == 401:
                    kind = FailureKind.AUTHENTICATION
                elif status == 403:
                    kind = FailureKind.AUTHORIZATION
                elif status in (400, 404):
                    kind = FailureKind.VALIDATION
                return _branch("providerRejected", kind, f"Bmob rejected the request with HTTP {status}")
        finally:
            await response.aclose()
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Bmob returned an invalid count response")
        if (
            not isinstance(document, dict)
            or type(document.get("count")) is not int
            or document["count"] < 0
            or document["count"] > 2**63 - 1
            or not isinstance(document.get("results"), list)
            or document["results"]
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Bmob returned an invalid count response")
        return QueryAttempt.branch_result("counted", CountClassObjectsOutput(document["count"]), Receipt())
