# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Klaviyo profile reads and deduplicated event writes with Go-compatible classification."""

import hashlib
import json
import re
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any
from urllib.parse import quote

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
    ACCEPTED as ACCEPTED,
)
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    CREATE_EVENT_DEFINITION as CREATE_EVENT_DEFINITION,
)
from ._generated.definitions import (
    CREATE_EVENT_OPERATION_ID as CREATE_EVENT_OPERATION_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_PROFILE_DEFINITION as GET_PROFILE_DEFINITION,
)
from ._generated.definitions import (
    GET_PROFILE_OPERATION_ID as GET_PROFILE_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import CreateEventInput, EventAcceptance, GetProfileInput, Profile

API_REVISION = "2026-10-15"
_DEFAULT_BASE_URL = "https://a.klaviyo.com/api"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_EVENT_PAYLOAD_BYTES = 1 << 20
_EMAIL = re.compile(r"[A-Za-z0-9!#$%&'*+/=?^_`{|}~.-]+@[A-Za-z0-9](?:[A-Za-z0-9.-]*[A-Za-z0-9])?")


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Klaviyo configuration is required")
        if credentials is None:
            raise ValueError("Klaviyo credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Klaviyo timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def get_profile(self) -> "GetProfileOperation":
        """Bind the profile Query to this client."""
        return GetProfileOperation(self)

    def create_event(self) -> "CreateEventOperation":
        """Bind the deduplicated event Mutation to this client."""
        return CreateEventOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def build_request(
        self, call: Call, operation: str, method: str, path: str, body: bytes | None
    ) -> httpx.Request | Failure:
        """Build one authenticated JSON:API request, or return the defect Failure."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        key = "" if credentials is None else credentials.private_api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failure(
                operation, FailureKind.AUTHENTICATION, "Klaviyo credentials are unavailable or invalid"
            )
        try:
            return self.http_client.build_request(
                method,
                self.base_url + path,
                content=body,
                headers={
                    "Authorization": "Klaviyo-API-Key " + key,
                    "Accept": "application/vnd.api+json",
                    "Content-Type": "application/vnd.api+json",
                    "revision": API_REVISION,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _failure(operation, FailureKind.LOCAL_DEFECT, "Klaviyo request could not be built")


class GetProfileOperation:
    """Read one Klaviyo profile by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Klaviyo client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_PROFILE_DEFINITION

    async def invoke(self, call: Call, input: GetProfileInput) -> QueryAttempt[Profile]:
        """Validate the ID, read once, then select a branch or safe retry."""
        operation = GET_PROFILE_OPERATION_ID
        if (
            not isinstance(input, GetProfileInput)
            or not isinstance(input.id, str)
            or not input.id.strip()
            or any(character in input.id for character in "/?&#")
        ):
            return _query_branch(DEFECT, FailureKind.VALIDATION, "profile ID is invalid")
        request = await self.client.build_request(
            call, operation, "GET", "/profiles/" + quote(input.id, safe=""), None
        )
        if isinstance(request, Failure):
            return QueryAttempt.branch_result(DEFECT, Profile(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(operation, FailureKind.TRANSPORT, "Klaviyo profile response is unavailable")
            )
        try:
            status = response.status_code
            if _is_retry_status(status):
                return QueryAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Klaviyo profile lookup is temporarily unavailable",
                    ),
                    _retry_after_seconds(response),
                )
            if status == 404:
                return _query_branch(NOT_FOUND, FailureKind.NOT_FOUND, "Klaviyo profile was not found")
            if status != 200:
                return _query_branch(
                    PROVIDER_REJECTED,
                    _rejection_kind(status),
                    f"Klaviyo rejected profile lookup with HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _query_branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Klaviyo profile response exceeded the limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(operation, FailureKind.AVAILABILITY, "Klaviyo profile response was interrupted")
                )
            try:
                profile = _decode_profile(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _query_branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Klaviyo returned an invalid profile"
                )
            return QueryAttempt.branch_result(FOUND, profile, Receipt())
        finally:
            await response.aclose()


class CreateEventOperation:
    """Submit one event; Klaviyo deduplicates repeats by the stable unique_id."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Klaviyo client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return CREATE_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CreateEventInput) -> IdempotencyKey:
        """Use the SHA-256 hex digest of the call ID as the event unique_id."""
        return IdempotencyKey(hashlib.sha256(str(call_id).encode()).hexdigest())

    async def invoke(self, call: Call, input: CreateEventInput) -> MutationAttempt[EventAcceptance]:
        """Validate, send one event, then select a branch or safe retry."""
        operation = CREATE_EVENT_OPERATION_ID
        if (
            not isinstance(input, CreateEventInput)
            or not _is_event_email(input.email)
            or not isinstance(input.metric_name, str)
            or not input.metric_name.strip()
            or not call.idempotency_key
        ):
            return _mutation_branch(
                DEFECT, FailureKind.VALIDATION, "email, metric name, and stable call identity are required"
            )
        try:
            body = json.dumps(_event_document(input, str(call.idempotency_key))).encode()
        except (TypeError, ValueError):
            body = b""
        if not body or len(body) > _MAX_EVENT_PAYLOAD_BYTES:
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "event payload is invalid or too large")
        request = await self.client.build_request(call, operation, "POST", "/events", body)
        if isinstance(request, Failure):
            return MutationAttempt.branch_result(DEFECT, EventAcceptance(), Receipt(), request)
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return MutationAttempt.retry(
                _failure(operation, FailureKind.TRANSPORT, "Klaviyo event response is unavailable")
            )
        try:
            status = response.status_code
            if _is_retry_status(status):
                return MutationAttempt.retry(
                    _failure(
                        operation,
                        FailureKind.AVAILABILITY,
                        "Klaviyo event submission is temporarily unavailable",
                    ),
                    _retry_after_seconds(response),
                )
            if status != 202:
                return _mutation_branch(
                    PROVIDER_REJECTED,
                    _rejection_kind(status),
                    f"Klaviyo rejected event submission with HTTP {status}",
                )
            return MutationAttempt.branch_result(ACCEPTED, EventAcceptance(True), Receipt())
        finally:
            await response.aclose()


def _event_document(input: CreateEventInput, unique_id: str) -> dict[str, Any]:
    properties: dict[str, str] | None = dict(input.properties) if input.properties else None
    return {
        "data": {
            "type": "event",
            "attributes": {
                "properties": properties,
                "metric": {"data": {"type": "metric", "attributes": {"name": input.metric_name}}},
                "profile": {"data": {"type": "profile", "attributes": {"email": input.email}}},
                "unique_id": unique_id,
            },
        }
    }


def _decode_profile(document: Any) -> Profile:
    data = _object(_object(document).get("data") or {})
    attributes = _object(data.get("attributes") or {})
    profile_type = data.get("type")
    profile_id = data.get("id")
    email = attributes.get("email")
    email = "" if email is None else email
    if (
        profile_type != "profile"
        or not isinstance(profile_id, str)
        or not profile_id
        or not isinstance(email, str)
    ):
        raise ValueError
    return Profile(profile_id, email)


def _is_event_email(value: object) -> bool:
    return isinstance(value, str) and _EMAIL.fullmatch(value) is not None


def _is_retry_status(status: int) -> bool:
    return status in (408, 409, 429) or status >= 500


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _retry_after_seconds(response: httpx.Response) -> float:
    return parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _query_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[Profile]:
    return QueryAttempt.branch_result(
        branch, Profile(), Receipt(), _failure(GET_PROFILE_OPERATION_ID, kind, message)
    )


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[EventAcceptance]:
    return MutationAttempt.branch_result(
        branch, EventAcceptance(), Receipt(), _failure(CREATE_EVENT_OPERATION_ID, kind, message)
    )


def _object(value: Any) -> dict[str, Any]:
    if not isinstance(value, dict):
        raise ValueError
    return value


Config.__doc__ = "Klaviyo HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "Klaviyo private API key, resolved anew for every provider attempt."
