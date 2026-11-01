# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Mixpanel deduplicated event imports and bounded raw exports with Go-compatible classification."""

import base64
import hashlib
import json
import re
from dataclasses import dataclass
from datetime import UTC, date, datetime, timedelta
from typing import Any
from urllib.parse import urlencode

import httpx
from dex_connectors.sdk import (
    BranchDefinition,
    Call,
    CallID,
    CredentialProvider,
    Failure,
    FailureKind,
    IdempotencyKey,
    MutationAttempt,
    MutationDefinition,
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

from .models import EventImportResult, ExportedEvent, ExportedEvents, ExportEventsInput, TrackEventInput

CONNECTOR_ID = "mixpanel"
TRACK_EVENT_OPERATION_ID = "trackEvent"
EXPORT_EVENTS_OPERATION_ID = "exportEvents"
IMPORTED = "imported"
EXPORTED = "exported"
PROVIDER_REJECTED = "providerRejected"
INVALID_RESPONSE = "invalidResponse"
DEFECT = "defect"
_DEFAULT_BASE_URL = "https://api.mixpanel.com"
_DEFAULT_EXPORT_BASE_URL = "https://data.mixpanel.com/api/2.0"
_DEFAULT_TIMEOUT_SECONDS = 15
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_MAX_IMPORT_PAYLOAD_BYTES = 1 << 20
_MAX_EXPORT_DAYS = 30
_ISO_DATE = re.compile(r"\d{4}-\d{2}-\d{2}")
_RESERVED_PROPERTY_NAMES = frozenset({"time", "distinct_id", "$insert_id", "ip"})
_STEP_DEFAULTS = StepDefaults(
    timedelta(seconds=30), 2, execute_retry=default_connector_step_execute_retry_policy()
)
_DEFECT_BRANCH = BranchDefinition(DEFECT, "Local input or credentials are invalid.", True)
TRACK_EVENT_DEFINITION = MutationDefinition(
    OperationRef(CONNECTOR_ID, TRACK_EVENT_OPERATION_ID),
    (
        BranchDefinition(IMPORTED, "Mixpanel accepted the event for ingestion."),
        BranchDefinition(PROVIDER_REJECTED, "Mixpanel rejected the event or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Mixpanel returned malformed or oversized data.", True),
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)
EXPORT_EVENTS_DEFINITION = QueryDefinition(
    OperationRef(CONNECTOR_ID, EXPORT_EVENTS_OPERATION_ID),
    (
        BranchDefinition(EXPORTED, "Mixpanel returned raw events, possibly empty."),
        BranchDefinition(PROVIDER_REJECTED, "Mixpanel rejected the export or credentials.", True),
        BranchDefinition(INVALID_RESPONSE, "Mixpanel returned malformed or oversized JSONL.", True),
        _DEFECT_BRANCH,
    ),
    _STEP_DEFAULTS,
)


@dataclass(frozen=True, slots=True)
class Config:
    """Mixpanel project and HTTP settings; zero or blank values use manifest defaults."""

    project_id: int
    base_url: str = _DEFAULT_BASE_URL
    export_base_url: str = _DEFAULT_EXPORT_BASE_URL
    timeout_seconds: float = _DEFAULT_TIMEOUT_SECONDS
    max_response_bytes: int = _DEFAULT_MAX_RESPONSE_BYTES


@dataclass(frozen=True, slots=True)
class Credentials:
    """Mixpanel service-account username and secret, resolved anew for every provider attempt."""

    service_account_username: SecretString
    service_account_secret: SecretString


@dataclass(frozen=True, slots=True)
class _ProviderReply:
    """A successful HTTP 200 body, or the non-200 status that selects providerRejected."""

    status: int
    body: bytes


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Mixpanel configuration is required")
        if credentials is None:
            raise ValueError("Mixpanel credential source is required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Mixpanel timeout and maxResponseBytes must be positive")
        if type(config.project_id) is not int or config.project_id <= 0:
            raise ValueError("Mixpanel project ID must be positive")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        self.export_base_url = validate_base_url(config.export_base_url or _DEFAULT_EXPORT_BASE_URL)
        self.project_id = config.project_id
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def track_event(self) -> "TrackEventOperation":
        """Bind the deduplicated event import Mutation to this client."""
        return TrackEventOperation(self)

    def export_events(self) -> "ExportEventsOperation":
        """Bind the bounded raw export Query to this client."""
        return ExportEventsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def send(
        self, call: Call, operation: str, method: str, address: str, body: bytes | None
    ) -> _ProviderReply | Failure | tuple[Failure, float]:
        """Send one request; a tuple means retry, a Failure with kind AUTHENTICATION or
        LOCAL_DEFECT means defect, and any other Failure means invalidResponse."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            credentials = None
        username = "" if credentials is None else credentials.service_account_username.reveal()
        secret = "" if credentials is None else credentials.service_account_secret.reveal()
        if (
            not username
            or not secret
            or not is_header_safe_credential(username)
            or not is_header_safe_credential(secret)
        ):
            return _failure(
                operation, FailureKind.AUTHENTICATION, "Mixpanel credentials are unavailable or invalid"
            )
        basic = base64.b64encode(f"{username}:{secret}".encode()).decode()
        headers = {
            "Authorization": "Basic " + basic,
            "Accept": "text/plain" if method == "GET" else "application/json",
        }
        if method == "POST":
            headers["Content-Type"] = "application/json"
        try:
            request = self.http_client.build_request(method, address, content=body, headers=headers)
        except (httpx.HTTPError, ValueError, TypeError):
            return _failure(operation, FailureKind.LOCAL_DEFECT, "Mixpanel request could not be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _failure(operation, FailureKind.TRANSPORT, "Mixpanel response is unavailable"), 0.0
        try:
            status = response.status_code
            if status in (408, 409, 429) or status >= 500:
                return (
                    _failure(operation, FailureKind.AVAILABILITY, "Mixpanel is temporarily unavailable"),
                    parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC)),
                )
            if status != 200:
                return _ProviderReply(status, b"")
            try:
                return _ProviderReply(status, await read_bounded_body(response, self.max_response_bytes))
            except BodyTooLargeError:
                return _failure(
                    operation, FailureKind.RESPONSE_TOO_LARGE, "Mixpanel response exceeded the limit"
                )
            except ProviderTransportError:
                return _failure(operation, FailureKind.AVAILABILITY, "Mixpanel response was interrupted"), 0.0
        finally:
            await response.aclose()


class TrackEventOperation:
    """Import one event; Mixpanel deduplicates repeats by event, time, distinct_id, and $insert_id."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Mixpanel client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return TRACK_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: TrackEventInput) -> IdempotencyKey:
        """Derive a UUID-shaped 36-byte $insert_id from the SHA-256 of Dex's call ID."""
        part = hashlib.sha256(str(call_id).encode()).hexdigest()[:32]
        return IdempotencyKey(f"{part[:8]}-{part[8:12]}-{part[12:16]}-{part[16:20]}-{part[20:]}")

    async def invoke(self, call: Call, input: TrackEventInput) -> MutationAttempt[EventImportResult]:
        """Validate, import one event strictly, then select a branch or safe retry."""
        operation = TRACK_EVENT_OPERATION_ID
        if (
            not isinstance(input, TrackEventInput)
            or not isinstance(input.event, str)
            or not input.event.strip()
            or not isinstance(input.distinct_id, str)
            or not input.distinct_id.strip()
            or type(input.time) is not int
            or input.time <= 0
            or len(str(call.idempotency_key or "")) != 36
        ):
            return _mutation_branch(
                DEFECT,
                FailureKind.VALIDATION,
                "event, distinctID, time, and stable call identity are required",
            )
        properties: dict[str, Any] = {
            "time": input.time,
            "distinct_id": input.distinct_id,
            "$insert_id": str(call.idempotency_key),
            "ip": 0,
        }
        for key, value in (input.properties or {}).items():
            if not isinstance(key, str) or key in _RESERVED_PROPERTY_NAMES or not key.strip():
                return _mutation_branch(
                    DEFECT, FailureKind.VALIDATION, "event properties include a reserved or blank name"
                )
            properties[key] = value
        try:
            payload = json.dumps(
                [{"event": input.event, "properties": properties}], separators=(",", ":")
            ).encode()
        except (TypeError, ValueError):
            payload = b""
        if not payload or len(payload) >= _MAX_IMPORT_PAYLOAD_BYTES:
            return _mutation_branch(DEFECT, FailureKind.VALIDATION, "event payload is invalid or too large")
        query = urlencode({"project_id": str(self.client.project_id), "strict": "1"})
        reply = await self.client.send(
            call, operation, "POST", f"{self.client.base_url}/import?{query}", payload
        )
        if isinstance(reply, tuple):
            return MutationAttempt.retry(reply[0], reply[1])
        if isinstance(reply, Failure):
            branch = DEFECT if _is_local_failure(reply) else INVALID_RESPONSE
            return MutationAttempt.branch_result(branch, EventImportResult(), Receipt(), reply)
        if reply.status != 200:
            return _mutation_branch(
                PROVIDER_REJECTED,
                _rejection_kind(reply.status),
                f"Mixpanel rejected event import with HTTP {reply.status}",
            )
        if not _is_import_confirmation(reply.body):
            return _mutation_branch(
                INVALID_RESPONSE, FailureKind.PROTOCOL, "Mixpanel returned an invalid import confirmation"
            )
        return MutationAttempt.branch_result(IMPORTED, EventImportResult(True), Receipt())


class ExportEventsOperation:
    """Download bounded raw JSONL events without arbitrary properties."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Mixpanel client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return EXPORT_EVENTS_DEFINITION

    async def invoke(self, call: Call, input: ExportEventsInput) -> QueryAttempt[ExportedEvents]:
        """Validate the range, export once, then select a branch or safe retry."""
        operation = EXPORT_EVENTS_OPERATION_ID
        if not isinstance(input, ExportEventsInput) or not _is_valid_export_input(input):
            return _query_branch(DEFECT, FailureKind.VALIDATION, "date range and limit must be valid")
        query = urlencode(
            {
                "from_date": input.from_date,
                "limit": str(input.limit),
                "project_id": str(self.client.project_id),
                "to_date": input.to_date,
            }
        )
        reply = await self.client.send(
            call, operation, "GET", f"{self.client.export_base_url}/export?{query}", None
        )
        if isinstance(reply, tuple):
            return QueryAttempt.retry(reply[0], reply[1])
        if isinstance(reply, Failure):
            branch = DEFECT if _is_local_failure(reply) else INVALID_RESPONSE
            return QueryAttempt.branch_result(branch, ExportedEvents(), Receipt(), reply)
        if reply.status != 200:
            return _query_branch(
                PROVIDER_REJECTED,
                _rejection_kind(reply.status),
                f"Mixpanel rejected raw export with HTTP {reply.status}",
            )
        events: list[ExportedEvent] = []
        for line in reply.body.split(b"\n"):
            if not line.strip():
                continue
            try:
                event = _decode_exported_event(json.loads(line))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _query_branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Mixpanel returned invalid JSONL"
                )
            events.append(event)
            if len(events) > input.limit:
                return _query_branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Mixpanel returned more events than requested"
                )
        return QueryAttempt.branch_result(EXPORTED, ExportedEvents(tuple(events)), Receipt())


def _is_valid_export_input(input: ExportEventsInput) -> bool:
    if type(input.limit) is not int or not 1 <= input.limit <= 100:
        return False
    if not all(
        isinstance(value, str) and _ISO_DATE.fullmatch(value) for value in (input.from_date, input.to_date)
    ):
        return False
    try:
        from_date = date.fromisoformat(input.from_date)
        to_date = date.fromisoformat(input.to_date)
    except ValueError:
        return False
    return from_date <= to_date and (to_date - from_date).days <= _MAX_EXPORT_DAYS


def _is_import_confirmation(body: bytes) -> bool:
    try:
        answer = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    return (
        isinstance(answer, dict)
        and answer.get("code") == 200
        and answer.get("num_records_imported") == 1
        and answer.get("status") == "OK"
    )


def _decode_exported_event(document: Any) -> ExportedEvent:
    if not isinstance(document, dict):
        raise ValueError
    event = document.get("event")
    properties = document.get("properties") or {}
    if not isinstance(event, str) or not event or not isinstance(properties, dict):
        raise ValueError
    distinct_id = properties.get("distinct_id") or ""
    time = properties.get("time") or 0
    insert_id = properties.get("$insert_id") or ""
    if not isinstance(distinct_id, str) or not isinstance(insert_id, str) or type(time) is not int:
        raise ValueError
    return ExportedEvent(event, distinct_id, time, insert_id)


def _is_local_failure(failure: Failure) -> bool:
    return failure.kind in (FailureKind.AUTHENTICATION, FailureKind.LOCAL_DEFECT)


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _failure(operation: str, kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, operation, message)


def _query_branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ExportedEvents]:
    return QueryAttempt.branch_result(
        branch, ExportedEvents(), Receipt(), _failure(EXPORT_EVENTS_OPERATION_ID, kind, message)
    )


def _mutation_branch(branch: str, kind: FailureKind, message: str) -> MutationAttempt[EventImportResult]:
    return MutationAttempt.branch_result(
        branch, EventImportResult(), Receipt(), _failure(TRACK_EVENT_OPERATION_ID, kind, message)
    )
