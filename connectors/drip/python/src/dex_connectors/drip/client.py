# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Drip reads with Go-compatible decisions."""

import base64
import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
from urllib.parse import quote

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
from ._generated.definitions import GET_SUBSCRIBER_DEFINITION, LIST_ACCOUNTS_DEFINITION
from .models import AccountMetadata, AccountsList, GetSubscriberInput, ListAccountsInput, SubscriberMetadata

Output = TypeVar("Output")
USER_AGENT = "Dex Connector (https://superdurable.io)"


@dataclass(frozen=True, slots=True)
class _Outcome:
    """One classified provider failure or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply a provider decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _failure(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "drip", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Drip returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Drip API token per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Drip configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Drip response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Drip timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_accounts(self) -> "ListAccountsOperation":
        """Bind the account discovery Query."""
        return ListAccountsOperation(self)

    def get_subscriber(self) -> "GetSubscriberOperation":
        """Bind the subscriber metadata Query."""
        return GetSubscriberOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or classify a provider failure without retaining its body."""
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            if not is_header_safe_credential(api_key):
                raise ValueError("invalid API token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation,
                "defect",
                FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        basic = base64.b64encode((api_key + ":").encode("utf-8")).decode("ascii")
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Basic " + basic,
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "drip", operation, "Drip request failed"), True
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "drip", operation, "Drip request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(FailureKind.RATE_LIMIT, "drip", operation, "Drip rate limited the request"),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(FailureKind.AVAILABILITY, "drip", operation, "Drip is temporarily unavailable"),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Drip did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Drip rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Drip response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(FailureKind.TRANSPORT, "drip", operation, "Drip response was interrupted"),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Drip returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListAccountsOperation:
    """List Drip account IDs and names without private fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_ACCOUNTS_DEFINITION

    async def invoke(self, call: Call, input: ListAccountsInput) -> QueryAttempt[AccountsList]:
        """Read accessible accounts or select a Go-compatible optional branch."""
        empty = AccountsList()
        if not isinstance(input, ListAccountsInput):
            return _failure(
                "listAccounts", "defect", FailureKind.VALIDATION, "account list input is invalid"
            ).with_empty(empty)
        response = await self.client.get(call, "listAccounts", "/accounts", False)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        records = response.get("accounts") if isinstance(response, dict) else None
        if not isinstance(records, list):
            return _invalid("listAccounts", empty)
        accounts = []
        for item in records:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("id"), str)
                or not item["id"]
                or not isinstance(item.get("name"), str)
                or not item["name"]
            ):
                return _invalid("listAccounts", empty)
            accounts.append(AccountMetadata(item["id"], item["name"]))
        return QueryAttempt.branch_result("listed", AccountsList(tuple(accounts)), Receipt())


class GetSubscriberOperation:
    """Read one subscriber's identity and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_SUBSCRIBER_DEFINITION

    async def invoke(self, call: Call, input: GetSubscriberInput) -> QueryAttempt[SubscriberMetadata]:
        """Read a subscriber after validating account and subscriber identifiers."""
        empty = SubscriberMetadata()
        if not isinstance(input, GetSubscriberInput) or not _valid_subscriber_input(input):
            return _failure(
                "getSubscriber",
                "defect",
                FailureKind.VALIDATION,
                "account ID or subscriber identifier is invalid",
            ).with_empty(empty)
        path = f"/{input.account_id}/subscribers/{quote(input.subscriber_id_or_email, safe='@')}"
        response = await self.client.get(call, "getSubscriber", path, True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        records = response.get("subscribers") if isinstance(response, dict) else None
        if not isinstance(records, list) or len(records) != 1:
            return _invalid("getSubscriber", empty)
        record = records[0]
        if (
            not isinstance(record, dict)
            or not isinstance(record.get("id"), str)
            or not record["id"]
            or not isinstance(record.get("email"), str)
            or not record["email"]
            or not isinstance(record.get("status"), str)
            or not record["status"]
            or (record.get("first_name") is not None and not isinstance(record["first_name"], str))
            or (record.get("last_name") is not None and not isinstance(record["last_name"], str))
        ):
            return _invalid("getSubscriber", empty)
        return QueryAttempt.branch_result(
            "found",
            SubscriberMetadata(
                record["id"],
                record["email"],
                record.get("first_name") or "",
                record.get("last_name") or "",
                record["status"],
            ),
            Receipt(),
        )


def _valid_subscriber_input(input: GetSubscriberInput) -> bool:
    account_id = input.account_id
    subscriber_id_or_email = input.subscriber_id_or_email
    if (
        not isinstance(account_id, str)
        or not re.fullmatch(r"[0-9]+", account_id)
        or len(account_id) > 20
        or not 0 < int(account_id) <= (1 << 64) - 1
        or not isinstance(subscriber_id_or_email, str)
        or not subscriber_id_or_email
        or subscriber_id_or_email.strip() != subscriber_id_or_email
    ):
        return False
    try:
        return len(subscriber_id_or_email.encode("utf-8")) <= 512
    except UnicodeEncodeError:
        return False
