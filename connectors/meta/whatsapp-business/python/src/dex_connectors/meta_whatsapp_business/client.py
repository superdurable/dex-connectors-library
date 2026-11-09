# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""WhatsApp Business Graph API reads and credential resolution."""

import json
import re
from datetime import UTC, datetime
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
from ._generated.definitions import LIST_PHONE_NUMBERS_DEFINITION
from ._graph_error import decode_graph_error
from .models import BusinessPhoneNumber, ListPhoneNumbersInput, PhoneNumbersPage
from .send import SendTemplateMessageOperation, SendTextMessageOperation

_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "whatsapp-business", "listPhoneNumbers", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[PhoneNumbersPage]:
    return QueryAttempt.branch_result(branch, PhoneNumbersPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[PhoneNumbersPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve a system-user token per call and send bounded Graph API requests."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("WhatsApp Business configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Meta response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Meta timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_phone_numbers(self) -> "ListPhoneNumbersOperation":
        """Bind the phone-number Query."""
        return ListPhoneNumbersOperation(self)

    def send_text_message(self) -> SendTextMessageOperation:
        """Bind the plain-text Mutation."""
        return SendTextMessageOperation(self)

    def send_template_message(self) -> SendTemplateMessageOperation:
        """Bind the approved-template Mutation."""
        return SendTemplateMessageOperation(self)

    async def aclose(self) -> None:
        """Close an owned HTTP client."""
        if self._owns_client:
            await self.http_client.aclose()


class ListPhoneNumbersOperation:
    """Read one bounded WABA phone-number page."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated phone-number contract."""
        return LIST_PHONE_NUMBERS_DEFINITION

    async def invoke(self, call: Call, input: ListPhoneNumbersInput) -> QueryAttempt[PhoneNumbersPage]:
        """Project phone identities or classify the provider decision."""
        if (
            not isinstance(input, ListPhoneNumbersInput)
            or _NUMERIC_ID.fullmatch(input.business_account_id) is None
            or not isinstance(input.after, str)
            or len(input.after) > 4096
            or any(character in input.after for character in "\r\n\x00")
        ):
            return _branch("defect", FailureKind.VALIDATION, "WABA ID or page cursor is invalid")
        path = f"/{input.business_account_id}/phone_numbers"
        if input.after:
            path += "?" + urlencode({"after": input.after})
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.access_token.reveal()
            if not is_header_safe_credential(token):
                raise ValueError("invalid system-user token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + token,
                    "User-Agent": "Dex Connector (https://superdurable.io)",
                },
            )
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _retry(FailureKind.TRANSPORT, "Meta request failed")
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            body = b""
            body_error: Exception | None = None
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except (BodyTooLargeError, ProviderTransportError) as error:
                body_error = error
            if token.encode() in body:
                return _branch(
                    "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
                )
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "Meta rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "Meta is temporarily unavailable", retry_after)
            graph_error = decode_graph_error(body) if status >= 400 and body_error is None else None
            if graph_error is not None and graph_error.is_throttled:
                return _retry(FailureKind.RATE_LIMIT, "Meta rate limited the request", retry_after)
            if graph_error is not None and graph_error.is_transient:
                return _retry(FailureKind.AVAILABILITY, "Meta reported a transient error", retry_after)
            if isinstance(body_error, BodyTooLargeError):
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meta response exceeded the configured limit",
                )
            if body_error is not None:
                return _retry(FailureKind.TRANSPORT, "Meta response was interrupted")
            if status == 404:
                return _branch(
                    "notFound", FailureKind.NOT_FOUND, "Meta did not find the WhatsApp Business Account"
                )
            if status != 200:
                return _branch(
                    "providerRejected", FailureKind.PROVIDER_REJECTION, f"Meta rejected HTTP {status}"
                )
            try:
                document = json.loads(body)
                if not isinstance(document, dict) or not isinstance(document.get("data"), list):
                    raise ValueError("missing phone-number data")
                data = document["data"]
                if len(data) > 100:
                    raise ValueError("oversized phone-number page")
                paging = document.get("paging") or {}
                cursors = paging.get("cursors") or {}
                after = cursors.get("after") or ""
                if not isinstance(after, str) or len(after) > 4096:
                    raise ValueError("invalid cursor")
                numbers = []
                for item in data:
                    if (
                        not isinstance(item, dict)
                        or not isinstance(item.get("id"), str)
                        or _NUMERIC_ID.fullmatch(item["id"]) is None
                        or not isinstance(item.get("verified_name"), str)
                        or not item["verified_name"]
                        or not isinstance(item.get("display_phone_number"), str)
                        or not item["display_phone_number"]
                    ):
                        raise ValueError("invalid phone-number identity")
                    numbers.append(
                        BusinessPhoneNumber(item["id"], item["verified_name"], item["display_phone_number"])
                    )
                return QueryAttempt.branch_result(
                    "listed", PhoneNumbersPage(tuple(numbers), after), Receipt()
                )
            except (ValueError, TypeError, AttributeError):
                return _branch(
                    "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid phone-number JSON"
                )
        finally:
            await response.aclose()
