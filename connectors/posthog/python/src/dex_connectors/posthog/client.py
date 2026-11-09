# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""PostHog capture and insight operations with Go-compatible provider decisions."""

import hashlib
import json
from datetime import UTC, datetime
from typing import Any

import httpx
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

from ._generated.config import Config as _GeneratedConfig
from ._generated.config import Credentials as _GeneratedCredentials
from ._generated.definitions import ACCEPTED as ACCEPTED
from ._generated.definitions import CAPTURE_EVENT_DEFINITION as CAPTURE_EVENT_DEFINITION
from ._generated.definitions import CAPTURE_EVENT_OPERATION_ID as CAPTURE_EVENT_OPERATION_ID
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_INSIGHTS_DEFINITION as LIST_INSIGHTS_DEFINITION
from ._generated.definitions import LIST_INSIGHTS_OPERATION_ID as LIST_INSIGHTS_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from ._generated.definitions import QUOTA_LIMITED as QUOTA_LIMITED
from .models import CapturedEvent, CaptureEventInput, InsightMetadata, InsightsPage, ListInsightsInput


class Config(_GeneratedConfig):
    """PostHog public and private API endpoints and HTTP response limits."""

    __slots__ = ()


class Credentials(_GeneratedCredentials):
    """Secret project and personal keys, resolved afresh for each provider attempt."""

    __slots__ = ()


class Client:
    """Own a redirect-safe provider client and immutable PostHog settings."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("PostHog configuration and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("PostHog timeout and response limit must be positive")
        self.capture_url = validate_base_url(config.capture_url or "https://us.i.posthog.com")
        self.app_url = validate_base_url(config.app_url or "https://us.posthog.com")
        self.max_response_bytes = config.max_response_bytes or 2_097_152
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds or 15)

    def capture_event(self) -> "CaptureEventOperation":
        """Bind the deduplicated event Mutation to this provider client."""
        return CaptureEventOperation(self)

    def list_insights(self) -> "ListInsightsOperation":
        """Bind the saved insight Query to this provider client."""
        return ListInsightsOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def request(
        self, method: str, address: str, headers: dict[str, str], body: bytes | None
    ) -> httpx.Response | None:
        """Send one provider request; a missing response means a transport failure."""
        request = self.http_client.build_request(method, address, headers=headers, content=body)
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return None
        return response


class CaptureEventOperation:
    """Send one event with a UUID derived from the stable Dex call identity."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PostHog client is required")
        self.client = client

    def definition(self) -> MutationDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return CAPTURE_EVENT_DEFINITION

    def idempotency_key(self, call_id: CallID, input: CaptureEventInput) -> IdempotencyKey:
        """Derive PostHog's stable UUID from the SHA-256 of Dex's call ID."""
        part = hashlib.sha256(str(call_id).encode()).hexdigest()[:32]
        return IdempotencyKey(f"{part[:8]}-{part[8:12]}-{part[12:16]}-{part[16:20]}-{part[20:]}")

    async def invoke(self, call: Call, input: CaptureEventInput) -> MutationAttempt[CapturedEvent]:
        """Validate a stable event, send it once, then select a branch or safe retry."""
        if not _valid_event(input, str(call.idempotency_key or "")):
            return _capture_failure(
                DEFECT, FailureKind.VALIDATION, "event, distinct ID, or stable call identity is invalid"
            )
        try:
            timestamp = datetime.fromisoformat(input.timestamp.replace("Z", "+00:00"))
            if timestamp.tzinfo is None or timestamp == datetime.min.replace(tzinfo=UTC):
                raise ValueError
            timestamp_text = timestamp.astimezone(UTC).isoformat().replace("+00:00", "Z")
            if timestamp_text.endswith(".000000Z"):
                timestamp_text = timestamp_text.replace(".000000Z", "Z")
        except (ValueError, AttributeError):
            return _capture_failure(DEFECT, FailureKind.VALIDATION, "timestamp must be an RFC3339 instant")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.project_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            token = ""
        if not token or not is_header_safe_credential(token):
            return _capture_failure(
                DEFECT, FailureKind.AUTHENTICATION, "PostHog project token is unavailable or invalid"
            )
        payload: dict[str, Any] = {
            "token": token,
            "event": input.event,
            "distinct_id": input.distinct_id,
            "timestamp": timestamp_text,
            "uuid": str(call.idempotency_key),
        }
        if input.properties:
            payload["properties"] = input.properties
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
        if len(body) > 1 << 20:
            return _capture_failure(DEFECT, FailureKind.VALIDATION, "event payload exceeds the limit")
        response = await self.client.request(
            "POST",
            self.client.capture_url + "/i/v0/e/",
            {"Accept": "application/json", "Content-Type": "application/json"},
            body,
        )
        if response is None:
            return _capture_retry(FailureKind.TRANSPORT, "PostHog capture response is unavailable")
        try:
            status = response.status_code
            after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _capture_retry(FailureKind.RATE_LIMIT, "PostHog rate limited capture", after)
            if status == 408 or status >= 500:
                return _capture_retry(
                    FailureKind.AVAILABILITY, "PostHog capture is temporarily unavailable", after
                )
            if status != 200:
                return _capture_failure(
                    PROVIDER_REJECTED, _rejection_kind(status), f"PostHog rejected capture with HTTP {status}"
                )
            try:
                answer = json.loads(await read_bounded_body(response, self.client.max_response_bytes))
            except BodyTooLargeError:
                return _capture_retry(
                    FailureKind.RESPONSE_TOO_LARGE, "PostHog capture confirmation exceeded the limit"
                )
            except ProviderTransportError:
                return _capture_retry(FailureKind.TRANSPORT, "PostHog capture confirmation was interrupted")
            except (ValueError, UnicodeDecodeError):
                return _capture_retry(FailureKind.PROTOCOL, "PostHog capture confirmation is unusable")
            if not isinstance(answer, dict) or answer.get("status") != "Ok":
                return _capture_retry(FailureKind.PROTOCOL, "PostHog capture confirmation is unusable")
            if answer.get("quota_limited") not in (None, [], {}):
                return _capture_failure(
                    QUOTA_LIMITED, FailureKind.PROVIDER_REJECTION, "PostHog reported an event quota limit"
                )
            return MutationAttempt.branch_result(
                ACCEPTED, CapturedEvent(str(call.idempotency_key), True), Receipt()
            )
        finally:
            await response.aclose()


class ListInsightsOperation:
    """Read one bounded page of saved insight identities without private metadata."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("PostHog client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_INSIGHTS_DEFINITION

    async def invoke(self, call: Call, input: ListInsightsInput) -> QueryAttempt[InsightsPage]:
        """Validate the page request and classify a single provider response."""
        if (
            not isinstance(input, ListInsightsInput)
            or type(input.project_id) is not int
            or input.project_id <= 0
            or type(input.offset) is not int
            or input.offset < 0
            or type(input.limit) is not int
            or not 0 <= input.limit <= 100
        ):
            return _insights_failure(
                DEFECT, FailureKind.VALIDATION, "project ID, offset, or limit is invalid"
            )
        limit = input.limit or 20
        try:
            credentials = await self.client.credentials.resolve(call)
            key = credentials.personal_api_key.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            key = ""
        if not key or not is_header_safe_credential(key):
            return _insights_failure(
                DEFECT, FailureKind.AUTHENTICATION, "PostHog personal API key is unavailable or invalid"
            )
        address = (
            f"{self.client.app_url}/api/projects/{input.project_id}/insights/"
            f"?limit={limit}&offset={input.offset}"
        )
        response = await self.client.request(
            "GET", address, {"Accept": "application/json", "Authorization": "Bearer " + key}, None
        )
        if response is None:
            return _insights_retry(FailureKind.TRANSPORT, "PostHog insight response is unavailable")
        try:
            status = response.status_code
            after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _insights_retry(FailureKind.RATE_LIMIT, "PostHog rate limited insight listing", after)
            if status == 408 or status >= 500:
                return _insights_retry(
                    FailureKind.AVAILABILITY, "PostHog insight listing is temporarily unavailable", after
                )
            if status != 200:
                return _insights_failure(
                    PROVIDER_REJECTED,
                    _rejection_kind(status),
                    f"PostHog rejected insight listing with HTTP {status}",
                )
            try:
                answer = json.loads(await read_bounded_body(response, self.client.max_response_bytes))
            except BodyTooLargeError:
                return _insights_failure(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "PostHog insight page exceeded the limit",
                )
            except ProviderTransportError:
                return _insights_retry(FailureKind.TRANSPORT, "PostHog insight response was interrupted")
            except (ValueError, UnicodeDecodeError):
                return _insights_failure(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "PostHog returned an invalid insight page"
                )
            try:
                page = _decode_insights_page(answer, input.offset, limit)
            except (ValueError, TypeError):
                return _insights_failure(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "PostHog returned an invalid insight page"
                )
            return QueryAttempt.branch_result(LISTED, page, Receipt())
        finally:
            await response.aclose()


def _valid_event(input: CaptureEventInput, key: str) -> bool:
    if (
        not isinstance(input, CaptureEventInput)
        or not isinstance(input.event, str)
        or not isinstance(input.distinct_id, str)
        or not input.event.strip()
        or not input.distinct_id.strip()
        or len(input.event.encode()) > 256
        or len(input.distinct_id.encode()) > 512
        or len(key) != 36
        or not isinstance(input.properties, dict)
    ):
        return False
    for name, value in input.properties.items():
        if (
            not isinstance(name, str)
            or not isinstance(value, str)
            or not name.strip()
            or len(name.encode()) > 128
            or len(value.encode()) > 4096
            or name.startswith("$")
            or name.lower() in {"token", "api_key", "password", "secret"}
        ):
            return False
    return True


def _decode_insights_page(answer: Any, offset: int, limit: int) -> InsightsPage:
    if not isinstance(answer, dict) or not isinstance(answer.get("results"), list):
        raise ValueError("missing insight results")
    results = answer["results"]
    if len(results) > limit or not isinstance(answer.get("next"), (str, type(None))):
        raise ValueError("invalid insight page")
    insights: list[InsightMetadata] = []
    for item in results:
        if (
            not isinstance(item, dict)
            or type(item.get("id")) is not int
            or item["id"] <= 0
            or not isinstance(item.get("name"), (str, type(None)))
        ):
            raise ValueError("invalid insight metadata")
        insights.append(InsightMetadata(item["id"], item.get("name") or ""))
    return InsightsPage(tuple(insights), bool(answer.get("next")), offset, limit)


def _rejection_kind(status: int) -> FailureKind:
    if status == 401:
        return FailureKind.AUTHENTICATION
    if status == 403:
        return FailureKind.AUTHORIZATION
    return FailureKind.PROVIDER_REJECTION


def _capture_failure(branch: str, kind: FailureKind, message: str) -> MutationAttempt[CapturedEvent]:
    return MutationAttempt.branch_result(
        branch, CapturedEvent(), Receipt(), Failure(kind, CONNECTOR_ID, CAPTURE_EVENT_OPERATION_ID, message)
    )


def _capture_retry(kind: FailureKind, message: str, after: float = 0) -> MutationAttempt[CapturedEvent]:
    return MutationAttempt.retry(Failure(kind, CONNECTOR_ID, CAPTURE_EVENT_OPERATION_ID, message), after)


def _insights_failure(branch: str, kind: FailureKind, message: str) -> QueryAttempt[InsightsPage]:
    return QueryAttempt.branch_result(
        branch, InsightsPage(), Receipt(), Failure(kind, CONNECTOR_ID, LIST_INSIGHTS_OPERATION_ID, message)
    )


def _insights_retry(kind: FailureKind, message: str, after: float = 0) -> QueryAttempt[InsightsPage]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, LIST_INSIGHTS_OPERATION_ID, message), after)
