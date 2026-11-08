# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Kit reads with Go-compatible decisions."""

import json
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeGuard, TypeVar
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
from ._generated.definitions import GET_SUBSCRIBER_DEFINITION, LIST_SUBSCRIBERS_DEFINITION
from .models import GetSubscriberInput, ListSubscribersInput, SubscriberMetadata, SubscribersPage

Output = TypeVar("Output")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply the decision to an operation's empty output type."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "kit", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Kit returned invalid JSON"
    ).with_empty(empty)


def _is_subscriber_id(value: object) -> bool:
    return type(value) is int and 0 < value <= (1 << 63) - 1


def _cursor_and_size(input: ListSubscribersInput) -> tuple[str, int] | None:
    if not isinstance(input.after, str) or type(input.per_page) is not int:
        return None
    try:
        cursor = input.after.encode("utf-8")
    except UnicodeEncodeError:
        return None
    if len(cursor) > 2048 or input.per_page < 0 or input.per_page > 1000:
        return None
    return input.after, input.per_page or 20


class Client:
    """One redirect-safe provider transport with call-time credential resolution."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Kit configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Kit response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Kit timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_subscribers(self) -> "ListSubscribersOperation":
        """Bind the subscriber page Query."""
        return ListSubscribersOperation(self)

    def get_subscriber(self) -> "GetSubscriberOperation":
        """Bind the subscriber metadata Query."""
        return GetSubscriberOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or return a classified failure without exposing provider data."""
        try:
            credential = await self.credentials.resolve(call)
            token = credential.api_key.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid API key")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={"Accept": "application/json", "X-Kit-Api-Key": token},
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "kit", operation, "Kit request failed"),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "kit", operation, "Kit request failed"),
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
                        "kit",
                        operation,
                        "Kit rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "kit",
                        operation,
                        "Kit is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Kit did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Kit rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Kit response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT,
                        "kit",
                        operation,
                        "Kit response was interrupted",
                    ),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Kit returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListSubscribersOperation:
    """List one cursor page of Kit subscribers."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return LIST_SUBSCRIBERS_DEFINITION

    async def invoke(self, call: Call, input: ListSubscribersInput) -> QueryAttempt[SubscribersPage]:
        """Fetch one subscriber page or select a Go-compatible optional branch."""
        empty = SubscribersPage()
        pagination = _cursor_and_size(input) if isinstance(input, ListSubscribersInput) else None
        if pagination is None:
            return _failure(
                "listSubscribers", "defect", FailureKind.VALIDATION, "cursor or perPage is invalid"
            ).with_empty(empty)
        after, per_page = pagination
        query = {"per_page": str(per_page), "slim": "true"}
        if after:
            query["after"] = after
        response = await self.client.get(
            call, "listSubscribers", "/subscribers?" + urlencode(query), False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, dict):
            return _invalid("listSubscribers", empty)
        subscribers = response.get("subscribers")
        page = response.get("pagination")
        if not isinstance(subscribers, list) or not isinstance(page, dict):
            return _invalid("listSubscribers", empty)
        has_next = page.get("has_next_page", False)
        next_cursor = page.get("end_cursor", "")
        if type(has_next) is not bool or (has_next and (not isinstance(next_cursor, str) or not next_cursor)):
            return _invalid("listSubscribers", empty)
        if any(not _valid_subscriber(item) for item in subscribers):
            return _invalid("listSubscribers", empty)
        output = SubscribersPage(
            tuple(_subscriber_metadata(item) for item in subscribers),
            has_next,
            next_cursor if has_next else "",
            per_page,
        )
        return QueryAttempt.branch_result("listed", output, Receipt())


class GetSubscriberOperation:
    """Read one subscriber without embedded custom fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the manifest-generated Query contract."""
        return GET_SUBSCRIBER_DEFINITION

    async def invoke(self, call: Call, input: GetSubscriberInput) -> QueryAttempt[SubscriberMetadata]:
        """Fetch subscriber metadata after validating its numeric ID."""
        empty = SubscriberMetadata()
        if not isinstance(input, GetSubscriberInput) or not _is_subscriber_id(input.subscriber_id):
            return _failure(
                "getSubscriber", "defect", FailureKind.VALIDATION, "subscriber ID must be positive"
            ).with_empty(empty)
        response = await self.client.get(call, "getSubscriber", f"/subscribers/{input.subscriber_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        subscriber = response.get("subscriber") if isinstance(response, dict) else None
        if not _valid_subscriber(subscriber):
            return _invalid("getSubscriber", empty)
        return QueryAttempt.branch_result("found", _subscriber_metadata(subscriber), Receipt())


def _valid_subscriber(value: object) -> TypeGuard[dict[str, Any]]:
    return (
        isinstance(value, dict)
        and _is_subscriber_id(value.get("id"))
        and isinstance(value.get("email_address"), str)
        and bool(value["email_address"])
        and isinstance(value.get("state"), str)
        and bool(value["state"])
        and (value.get("first_name") is None or isinstance(value["first_name"], str))
    )


def _subscriber_metadata(value: dict[str, Any]) -> SubscriberMetadata:
    return SubscriberMetadata(
        value["id"], value["email_address"], value.get("first_name") or "", value["state"]
    )
