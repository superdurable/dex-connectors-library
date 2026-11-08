# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Copper reads with Go-compatible decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar

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
from ._generated.definitions import GET_LEAD_DEFINITION, LIST_LEAD_STATUSES_DEFINITION
from .models import GetLeadInput, LeadMetadata, LeadStatusesList, LeadStatusMetadata, ListLeadStatusesInput

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
    return _Outcome(branch, Failure(kind, "copper", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Copper returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Copper API key and owner email per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Copper configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Copper response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Copper timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_lead_statuses(self) -> "ListLeadStatusesOperation":
        """Bind the lead status discovery Query."""
        return ListLeadStatusesOperation(self)

    def get_lead(self) -> "GetLeadOperation":
        """Bind the lead metadata Query."""
        return GetLeadOperation(self)

    async def aclose(self) -> None:
        """Close the transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str, has_not_found: bool) -> Any:
        """Fetch JSON or classify a provider failure without retaining its body."""
        try:
            credential = await self.credentials.resolve(call)
            api_key = credential.api_key.reveal()
            user_email = credential.user_email.reveal()
            if (
                not is_header_safe_credential(api_key)
                or not is_header_safe_credential(user_email)
                or "@" not in user_email
                or user_email.strip() != user_email
            ):
                raise ValueError("invalid Copper credentials")
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
                headers={
                    "Accept": "application/json",
                    "Content-Type": "application/json",
                    "X-PW-AccessToken": api_key,
                    "X-PW-Application": "developer_api",
                    "X-PW-UserEmail": user_email,
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "copper", operation, "Copper request failed"), True
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "", Failure(FailureKind.TRANSPORT, "copper", operation, "Copper request failed"), True
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "",
                    Failure(FailureKind.RATE_LIMIT, "copper", operation, "Copper rate limited the request"),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY, "copper", operation,
                        "Copper is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Copper did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Copper rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Copper response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(FailureKind.TRANSPORT, "copper", operation, "Copper response was interrupted"),
                    True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Copper returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListLeadStatusesOperation:
    """List Copper lead status IDs and names without private fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_LEAD_STATUSES_DEFINITION

    async def invoke(self, call: Call, input: ListLeadStatusesInput) -> QueryAttempt[LeadStatusesList]:
        """Read visible statuses or select a Go-compatible optional branch."""
        empty = LeadStatusesList()
        if not isinstance(input, ListLeadStatusesInput):
            return _failure(
                "listLeadStatuses", "defect", FailureKind.VALIDATION, "lead status list input is invalid"
            ).with_empty(empty)
        response = await self.client.get(call, "listLeadStatuses", "/lead_statuses", False)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if not isinstance(response, list):
            return _invalid("listLeadStatuses", empty)
        statuses = []
        for item in response:
            if (
                not isinstance(item, dict)
                or type(item.get("id")) is not int
                or item["id"] <= 0
                or not isinstance(item.get("name"), str)
                or not item["name"]
            ):
                return _invalid("listLeadStatuses", empty)
            statuses.append(LeadStatusMetadata(item["id"], item["name"]))
        return QueryAttempt.branch_result("listed", LeadStatusesList(tuple(statuses)), Receipt())


class GetLeadOperation:
    """Read one Copper lead's identity and status."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_LEAD_DEFINITION

    async def invoke(self, call: Call, input: GetLeadInput) -> QueryAttempt[LeadMetadata]:
        """Read a lead after validating its positive decimal ID."""
        empty = LeadMetadata()
        if not isinstance(input, GetLeadInput) or not _valid_lead_id(input.lead_id):
            return _failure(
                "getLead", "defect", FailureKind.VALIDATION,
                "lead ID must be a positive decimal identifier",
            ).with_empty(empty)
        response = await self.client.get(call, "getLead", f"/leads/{input.lead_id}", True)
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or type(response.get("id")) is not int
            or response["id"] <= 0
            or not isinstance(response.get("name"), str)
            or not response["name"]
            or not isinstance(response.get("status"), str)
            or not response["status"]
            or (response.get("company_name") is not None and not isinstance(response["company_name"], str))
        ):
            return _invalid("getLead", empty)
        return QueryAttempt.branch_result(
            "found",
            LeadMetadata(
                response["id"], response["name"], response["status"],
                response.get("company_name") or "",
            ),
            Receipt(),
        )


def _valid_lead_id(lead_id: str) -> bool:
    return (
        isinstance(lead_id, str)
        and re.fullmatch(r"[0-9]+", lead_id) is not None
        and 0 < int(lead_id) <= (1 << 63) - 1
    )
