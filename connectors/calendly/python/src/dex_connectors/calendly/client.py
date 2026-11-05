# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Bounded Calendly transport and generated OAuth credential resolution."""

import json
from dataclasses import dataclass, field, replace
from datetime import UTC, datetime
from typing import Any
from urllib.parse import quote_plus

import httpx
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
    ReauthorizationRequiredError,
    Receipt,
    resolve_credential,
    resolve_credential_after_rejection,
)
from dex_connectors.sdk.oauthtoken import ManifestCredentialRefreshDriver
from dex_connectors.sdk.providerhttp import (
    MAX_ERROR_BODY_BYTES,
    BodyTooLargeError,
    ProviderTransportError,
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.refresh import AUTHORIZATION, CREDENTIAL_FIELDS

API_BASE = "https://api.calendly.com"


@dataclass(slots=True)
class Exchange:
    """Status and safe body of one provider exchange."""

    status: int = 0
    headers: httpx.Headers = field(default_factory=httpx.Headers)
    body: bytes = b""
    error: str = ""


def failure(operation: str, kind: FailureKind, message: str) -> Failure:
    """Build one secret-safe Calendly failure."""
    return Failure(kind, "calendly", operation, message)


class Client:
    """Run Calendly API operations with a call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Any = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Calendly configuration and credential provider are required")
        defaults = Config()
        config = replace(
            config,
            max_response_bytes=config.max_response_bytes or defaults.max_response_bytes,
            webhook_max_body_bytes=config.webhook_max_body_bytes or defaults.webhook_max_body_bytes,
            webhook_signature_tolerance_seconds=(
                config.webhook_signature_tolerance_seconds or defaults.webhook_signature_tolerance_seconds
            ),
        )
        if (
            config.max_response_bytes < 0
            or config.webhook_max_body_bytes < 0
            or config.webhook_signature_tolerance_seconds < 0
        ):
            raise ValueError("Calendly configuration limits cannot be negative")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(25)
        self.owns_http_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))
        self.refresh_driver = ManifestCredentialRefreshDriver[Credentials](
            "Calendly", AUTHORIZATION, CREDENTIAL_FIELDS, self.http_client
        )

    async def aclose(self) -> None:
        """Close the transport this client created."""
        if self.owns_http_client:
            await self.http_client.aclose()

    def list_scheduled_events(self) -> Any:
        """Bind the scheduled event listing Query."""
        from .operations import ListScheduledEventsOperation

        return ListScheduledEventsOperation(self)

    def get_scheduled_event(self) -> Any:
        """Bind the scheduled event read Query."""
        from .operations import GetScheduledEventOperation

        return GetScheduledEventOperation(self)

    def list_event_invitees(self) -> Any:
        """Bind the invitee listing Query."""
        from .operations import ListEventInviteesOperation

        return ListEventInviteesOperation(self)

    def cancel_scheduled_event(self) -> Any:
        """Bind the repeatable cancellation Mutation."""
        from .operations import CancelScheduledEventOperation

        return CancelScheduledEventOperation(self)

    def create_scheduling_link(self) -> Any:
        """Bind the fenced scheduling link Mutation."""
        from .operations import CreateSchedulingLinkOperation

        return CreateSchedulingLinkOperation(self)

    def create_webhook_subscription(self) -> Any:
        """Bind the list-before-create webhook Mutation."""
        from .operations import CreateWebhookSubscriptionOperation

        return CreateWebhookSubscriptionOperation(self)

    def receipt(self, call: Call, object_uri: str = "") -> Receipt:
        """Record the provider object ID without storing secrets."""
        return Receipt(call.id, "calendly", self.now().astimezone(UTC), call.idempotency_key, object_uri)

    async def resolve_credentials(self, call: Call) -> tuple[Credentials | None, FailureKind | None, str]:
        """Resolve private or shared credentials through generated OAuth rules."""
        try:
            credentials = await resolve_credential(self.credentials, call, self.refresh_driver)
        except ReauthorizationRequiredError:
            return (
                None,
                FailureKind.AUTHENTICATION,
                "the Calendly connection needs reauthorization; authorize it again in Dex Web Connectors",
            )
        except Exception:
            return (
                None,
                FailureKind.AVAILABILITY,
                "the Calendly access token could not be loaded or refreshed yet",
            )
        if (
            not isinstance(credentials, Credentials)
            or credentials.access_token is None
            or not is_header_safe_credential(credentials.access_token.reveal())
        ):
            return (
                None,
                FailureKind.AUTHENTICATION,
                "the Calendly access token is blank or contains characters a header cannot carry",
            )
        return credentials, None, ""

    async def exchange(
        self,
        call: Call,
        credentials: Credentials,
        method: str,
        path: str,
        query: dict[str, str] | None = None,
        payload: object | None = None,
        allow_refresh: bool = True,
    ) -> Exchange:
        """Send one call, forcing one OAuth refresh after a 401 when available."""
        target = API_BASE + path
        if query:
            target += "?" + "&".join(
                f"{quote_plus(key, safe='-._')}={quote_plus(value, safe='-._')}"
                for key, value in sorted(query.items())
            )
        body = None
        if payload is not None:
            try:
                body = json.dumps(
                    payload, separators=(",", ":"), ensure_ascii=False, allow_nan=False
                ).encode()
            except (ValueError, TypeError):
                return Exchange(error="defect")
        for attempt in range(2):
            assert credentials.access_token is not None
            headers = {
                "Authorization": "Bearer " + credentials.access_token.reveal(),
                "Accept": "application/json",
            }
            if body is not None:
                headers["Content-Type"] = "application/json"
            try:
                request = self.http_client.build_request(
                    method, target, headers=headers, content=body, timeout=25
                )
            except (httpx.HTTPError, ValueError):
                return Exchange(error="defect")
            try:
                response, _ = await send_traced(self.http_client, request)
            except ProviderTransportError as error:
                return Exchange(error="notSent" if error.provably_not_sent else "transport")
            try:
                limit = (
                    self.config.max_response_bytes
                    if 200 <= response.status_code < 300
                    else MAX_ERROR_BODY_BYTES
                )
                try:
                    contents = await read_bounded_body(response, limit)
                except BodyTooLargeError:
                    if 200 <= response.status_code < 300:
                        return Exchange(response.status_code, response.headers, error="tooLarge")
                    contents = b""
                except ProviderTransportError:
                    if 200 <= response.status_code < 300:
                        return Exchange(response.status_code, response.headers, error="transport")
                    contents = b""
                exchange = Exchange(response.status_code, response.headers, contents)
            finally:
                await response.aclose()
            if (
                exchange.status != 401
                or attempt
                or not allow_refresh
                or credentials.auth_method_id == "personal-access-token"
            ):
                return exchange
            try:
                credentials = await resolve_credential_after_rejection(
                    self.credentials, call, self.refresh_driver
                )
                if credentials.access_token is None or not is_header_safe_credential(
                    credentials.access_token.reveal()
                ):
                    return exchange
            except Exception:
                return exchange
        return Exchange(error="defect")

    def classify(self, operation: str, exchange: Exchange) -> tuple[Failure, float, bool]:
        """Classify a non-success status without exposing provider text."""
        status = exchange.status
        if status == 429:
            delay = parse_retry_after(exchange.headers.get("Retry-After", ""), self.now())
            if not delay:
                try:
                    seconds = int(exchange.headers.get("X-RateLimit-Reset", "").strip())
                    delay = min(seconds, 3600) if seconds > 0 else 0
                except ValueError:
                    delay = 0
            return (
                failure(operation, FailureKind.RATE_LIMIT, "Calendly rate limited the request"),
                delay,
                True,
            )
        if status >= 500:
            return failure(operation, FailureKind.AVAILABILITY, f"Calendly answered HTTP {status}"), 0, True
        if status == 401:
            return (
                failure(
                    operation,
                    FailureKind.AUTHENTICATION,
                    (
                        "Calendly rejected the access token; generate a new personal access token "
                        "or authorize the connection again"
                    ),
                ),
                0,
                False,
            )
        if status == 403:
            try:
                is_scope = json.loads(exchange.body).get("title") == "Insufficient scope"
            except (ValueError, AttributeError):
                is_scope = False
            message = (
                (
                    "the Calendly token lacks a scope this operation needs; grant the scopes "
                    "listed in the connector guide"
                )
                if is_scope
                else "Calendly denied permission for this user, organization, or resource"
            )
            return failure(operation, FailureKind.AUTHORIZATION, message), 0, False
        if status == 404:
            return failure(operation, FailureKind.NOT_FOUND, "the Calendly resource was not found"), 0, False
        if status == 409:
            return (
                failure(operation, FailureKind.CONFLICT, "Calendly reports that the resource already exists"),
                0,
                False,
            )
        return (
            failure(
                operation, FailureKind.PROVIDER_REJECTION, f"Calendly rejected the request with HTTP {status}"
            ),
            0,
            False,
        )
