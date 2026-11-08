# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Authenticated Zendesk Sell lead reads with Go-compatible decisions."""

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
from ._generated.definitions import GET_LEAD_DEFINITION, LIST_LEADS_DEFINITION
from .models import GetLeadInput, LeadMetadata, LeadsPage, ListLeadsInput

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
    return _Outcome(branch, Failure(kind, "zendesk-sell", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _failure(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Zendesk Sell returned invalid JSON"
    ).with_empty(empty)


class Client:
    """Resolve the Zendesk Sell access token per call and bound provider responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Zendesk Sell configuration and credential source are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Zendesk Sell response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Zendesk Sell timeout must be positive")
        self.base_url = validate_base_url(config.base_url or "https://api.getbase.com/v2").rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_leads(self) -> "ListLeadsOperation":
        """Bind the lead page Query."""
        return ListLeadsOperation(self)

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
            access_token = credential.access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid Zendesk Sell access token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _failure(
                operation, "defect", FailureKind.AUTHENTICATION,
                "connection credentials are unavailable or invalid",
            )
        try:
            request = self.http_client.build_request(
                "GET",
                self.base_url + path,
                headers={
                    "Accept": "application/json",
                    "Authorization": "Bearer " + access_token,
                    "User-Agent": USER_AGENT,
                },
            )
        except (httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "zendesk-sell", operation, "Zendesk Sell request failed"),
                True,
            )
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "zendesk-sell", operation, "Zendesk Sell request failed"),
                True,
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return _Outcome(
                    "", Failure(FailureKind.RATE_LIMIT, "zendesk-sell", operation,
                                "Zendesk Sell rate limited the request"),
                    True, retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "", Failure(FailureKind.AVAILABILITY, "zendesk-sell", operation,
                                "Zendesk Sell is temporarily unavailable"), True, retry_after,
                )
            if status == 404 and has_not_found:
                return _failure(
                    operation, "notFound", FailureKind.NOT_FOUND, "Zendesk Sell did not find the resource"
                )
            if status != 200:
                return _failure(
                    operation, "providerRejected", FailureKind.PROVIDER_REJECTION,
                    f"Zendesk Sell rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failure(
                    operation, "invalidResponse", FailureKind.RESPONSE_TOO_LARGE,
                    "Zendesk Sell response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "", Failure(FailureKind.TRANSPORT, "zendesk-sell", operation,
                                "Zendesk Sell response was interrupted"), True,
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _failure(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Zendesk Sell returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListLeadsOperation:
    """List one bounded page of Zendesk Sell lead identities."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_LEADS_DEFINITION

    async def invoke(self, call: Call, input: ListLeadsInput) -> QueryAttempt[LeadsPage]:
        """Read a lead page or select a Go-compatible optional branch."""
        empty = LeadsPage()
        if (
            not isinstance(input, ListLeadsInput)
            or type(input.page) is not int or input.page < 1
            or type(input.per_page) is not int or not 1 <= input.per_page <= 100
        ):
            return _failure(
                "listLeads", "defect", FailureKind.VALIDATION,
                "lead page number or size is invalid",
            ).with_empty(empty)
        response = await self.client.get(
            call, "listLeads", f"/leads?page={input.page}&per_page={input.per_page}", False
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        items = response.get("items") if isinstance(response, dict) else None
        if not isinstance(items, list):
            return _invalid("listLeads", empty)
        leads = []
        for item in items:
            lead = _lead_metadata(item.get("data") if isinstance(item, dict) else None)
            if lead is None:
                return _invalid("listLeads", empty)
            leads.append(lead)
        return QueryAttempt.branch_result(
            "listed", LeadsPage(tuple(leads), input.page, input.per_page), Receipt()
        )


class GetLeadOperation:
    """Read one Zendesk Sell lead's identity."""

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
        lead = _lead_metadata(response.get("data") if isinstance(response, dict) else None)
        if lead is None:
            return _invalid("getLead", empty)
        return QueryAttempt.branch_result("found", lead, Receipt())


def _valid_lead_id(lead_id: str) -> bool:
    return (
        isinstance(lead_id, str)
        and re.fullmatch(r"[0-9]+", lead_id) is not None
        and len(lead_id.lstrip("0")) <= 19
        and 0 < int(lead_id.lstrip("0") or "0") <= (1 << 63) - 1
    )


def _lead_metadata(value: Any) -> LeadMetadata | None:
    if not isinstance(value, dict) or type(value.get("id")) is not int or value["id"] <= 0:
        return None
    for key in ("first_name", "last_name", "email", "organization_name"):
        if value.get(key) is not None and not isinstance(value[key], str):
            return None
    return LeadMetadata(
        value["id"], value.get("first_name") or "", value.get("last_name") or "",
        value.get("email") or "", value.get("organization_name") or "",
    )
