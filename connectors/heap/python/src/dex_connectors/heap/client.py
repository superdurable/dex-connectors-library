# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Heap user-property updates with Go-compatible outcomes."""

from __future__ import annotations

import hashlib
import json
from datetime import UTC, datetime
from typing import Any

import httpx
from dex_connectors.sdk import (
    Call,
    CallID,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
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
from ._generated.definitions import SET_USER_PROPERTIES_DEFINITION
from .models import SetUserPropertiesInput, UserPropertiesResult


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "heap", "setUserProperties", message)


def _branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[UserPropertiesResult]:
    return MutationAttempt.branch_result(branch, UserPropertiesResult(), Receipt(), _failure(kind, message))


def _valid_app_id(app_id: str) -> bool:
    return isinstance(app_id, str) and 0 < len(app_id) <= 32 and all("0" <= digit <= "9" for digit in app_id)


def _utf8_length(value: object) -> int | None:
    if not isinstance(value, str):
        return None
    try:
        return len(value.encode("utf-8"))
    except UnicodeEncodeError:
        return None


def _valid_input(input: SetUserPropertiesInput) -> bool:
    if not isinstance(input, SetUserPropertiesInput):
        return False
    identity_length = _utf8_length(input.identity)
    if identity_length is None or not 0 < identity_length <= 255 or not input.identity.strip():
        return False
    properties = input.properties
    if not isinstance(properties, dict) or not 0 < len(properties) <= 50:
        return False
    for key, value in properties.items():
        key_length, value_length = _utf8_length(key), _utf8_length(value)
        if (
            key_length is None or value_length is None
            or not key.strip() or key.lower() == "id"
            or key_length >= 1024 or value_length >= 1024
        ):
            return False
    return True


class Client:
    """Send Heap requests without credentials; app ID selects the environment."""

    def __init__(self, config: Config, http_client: httpx.AsyncClient | None = None) -> None:
        if config is None or not _valid_app_id(config.app_id):
            raise ValueError("Heap appId must contain decimal digits")
        if (
            config.timeout_seconds <= 0
            or type(config.max_response_bytes) is not int
            or config.max_response_bytes < 1
        ):
            raise ValueError("Heap request limits must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.app_id = config.app_id
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def set_user_properties(self) -> SetUserPropertiesOperation:
        """Bind the Heap property update Mutation to this client."""
        return SetUserPropertiesOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client only when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class SetUserPropertiesOperation:
    """Overwrite selected string properties for a stable Heap identity."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Heap client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return the manifest-generated Mutation contract."""
        return SET_USER_PROPERTIES_DEFINITION

    def idempotency_key(self, call_id: CallID, _input: SetUserPropertiesInput) -> IdempotencyKey:
        """Derive the same stable call key as Go; absolute writes make retries safe."""
        return IdempotencyKey(hashlib.sha256(str(call_id).encode()).hexdigest())

    async def invoke(
        self, call: Call, input: SetUserPropertiesInput
    ) -> MutationAttempt[UserPropertiesResult]:
        """Validate and send one property update, then classify the provider outcome."""
        if not _valid_input(input):
            return _branch("defect", FailureKind.VALIDATION, "identity or properties are invalid")
        document: dict[str, Any] = {
            "app_id": self.client.app_id,
            "identity": input.identity,
            "properties": input.properties,
        }
        body = json.dumps(document, ensure_ascii=False, separators=(",", ":")).encode("utf-8")
        if len(body) > 65536:
            return _branch("defect", FailureKind.VALIDATION, "property payload exceeds the limit")
        try:
            request = self.client.http_client.build_request(
                "POST",
                self.client.base_url + "/api/add_user_properties",
                content=body,
                headers={"Content-Type": "application/json", "Accept": "application/json"},
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.LOCAL_DEFECT, "Heap request could not be built")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return MutationAttempt.retry(_failure(FailureKind.TRANSPORT, "Heap response is unavailable"))
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return MutationAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "Heap rate limited the request"), retry_after
                )
            if status == 408 or status >= 500:
                return MutationAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "Heap is temporarily unavailable"), retry_after
                )
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Heap rejected HTTP {status}"
                )
            try:
                await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse", FailureKind.RESPONSE_TOO_LARGE, "Heap confirmation exceeded the limit"
                )
            except ProviderTransportError:
                return MutationAttempt.retry(
                    _failure(FailureKind.TRANSPORT, "Heap confirmation was interrupted")
                )
            return MutationAttempt.branch_result("accepted", UserPropertiesResult(True), Receipt())
        finally:
            await response.aclose()
