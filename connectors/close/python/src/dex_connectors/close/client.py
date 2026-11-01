# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Close provider reads and Go-compatible branch classification."""

import base64
import json
import re
from collections.abc import Callable
from dataclasses import dataclass as dataclass
from datetime import UTC, datetime
from datetime import timedelta as timedelta
from typing import Any, TypeVar

import httpx
from dex_connectors.sdk import BranchDefinition as BranchDefinition
from dex_connectors.sdk import (
    Call,
    CredentialProvider,
    Failure,
    FailureKind,
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
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    FOUND as FOUND,
)
from ._generated.definitions import (
    GET_LEAD_DEFINITION as GET_LEAD_DEFINITION,
)
from ._generated.definitions import (
    GET_LEAD_OPERATION_ID as GET_LEAD_OPERATION_ID,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_LEADS_DEFINITION as LIST_LEADS_DEFINITION,
)
from ._generated.definitions import (
    LIST_LEADS_OPERATION_ID as LIST_LEADS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    NOT_FOUND as NOT_FOUND,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import GetLeadInput, Lead, LeadPage, ListLeadsInput

_DEFAULT_BASE_URL = "https://api.close.com/api/v1"
_DEFAULT_TIMEOUT_SECONDS = 30
_DEFAULT_MAX_RESPONSE_BYTES = 4_194_304
_SAFE_KEY = re.compile(r"[A-Za-z0-9_-]{1,128}")

Output = TypeVar("Output")


class Client:
    """One redirect-safe HTTP client and call-time credential source."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None:
            raise ValueError("Close configuration is required")
        if credentials is None:
            raise ValueError("Close credential source is required")
        self.base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("Close timeout and max response bytes must be positive")
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )

    def list_leads(self) -> "ListLeadsOperation":
        """Bind the lead page Query to this client."""
        return ListLeadsOperation(self)

    def get_lead(self) -> "GetLeadOperation":
        """Bind the lead lookup Query to this client."""
        return GetLeadOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after application shutdown."""
        await self.http_client.aclose()

    async def invoke_get(
        self,
        call: Call,
        operation: str,
        path: str,
        empty: Output,
        success: str,
        not_found: str,
        decode: Callable[[Any], Output],
    ) -> QueryAttempt[Output]:
        """Send one GET and classify it exactly like the Go invokeGet helper."""
        try:
            credentials = await self.credentials.resolve(call)
        except (ValueError, LookupError, OSError, ReauthorizationRequiredError):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are unavailable"
            )
        key = credentials.api_key.reveal()
        if not key or not is_header_safe_credential(key):
            return _failed(
                operation, DEFECT, empty, FailureKind.AUTHENTICATION, "connection credentials are invalid"
            )
        headers = {
            "Accept": "application/json",
            "Authorization": "Basic " + base64.b64encode((key + ":").encode()).decode(),
        }
        try:
            request = self.http_client.build_request("GET", self.base_url + path, headers=headers)
        except (httpx.HTTPError, ValueError):
            return _failed(operation, DEFECT, empty, FailureKind.LOCAL_DEFECT, "request cannot be built")
        try:
            response, _ = await send_traced(self.http_client, request)
        except ProviderTransportError:
            return _retry(operation, FailureKind.AVAILABILITY, "provider request failed", 0)
        try:
            status = response.status_code
            if status != 200:
                retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
                if status == 429:
                    return _retry(operation, FailureKind.RATE_LIMIT, "provider rate limit", retry_after)
                if status in (408, 409) or status >= 500:
                    return _retry(
                        operation, FailureKind.AVAILABILITY, "provider temporarily unavailable", retry_after
                    )
                if status == 404 and not_found != PROVIDER_REJECTED:
                    return _failed(
                        operation, not_found, empty, FailureKind.NOT_FOUND, "provider resource was not found"
                    )
                if status == 401:
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHENTICATION,
                        "provider rejected the API key",
                    )
                if status == 403:
                    return _failed(
                        operation,
                        PROVIDER_REJECTED,
                        empty,
                        FailureKind.AUTHORIZATION,
                        "provider denied the request",
                    )
                return _failed(
                    operation,
                    PROVIDER_REJECTED,
                    empty,
                    FailureKind.PROVIDER_REJECTION,
                    f"provider rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "provider response exceeded configured limit",
                )
            except ProviderTransportError:
                return _retry(operation, FailureKind.AVAILABILITY, "provider response was interrupted", 0)
            try:
                value = decode(json.loads(body))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _failed(
                    operation,
                    INVALID_RESPONSE,
                    empty,
                    FailureKind.PROTOCOL,
                    "provider returned unusable JSON",
                )
            return QueryAttempt.branch_result(success, value, Receipt())
        finally:
            await response.aclose()


class ListLeadsOperation:
    """Read one offset page of Close leads."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Close client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return LIST_LEADS_DEFINITION

    async def invoke(self, call: Call, input: ListLeadsInput) -> QueryAttempt[LeadPage]:
        """Validate the page window, read once, then select a branch or safe retry."""
        operation = LIST_LEADS_OPERATION_ID
        if (
            not isinstance(input, ListLeadsInput)
            or type(input.skip) is not int
            or type(input.limit) is not int
            or input.skip < 0
            or not 0 <= input.limit <= 100
        ):
            return _failed(operation, DEFECT, LeadPage(), FailureKind.VALIDATION, "skip or limit is invalid")
        path = f"/lead/?_skip={input.skip}&_limit={input.limit or 100}"
        return await self.client.invoke_get(
            call, operation, path, LeadPage(), LISTED, PROVIDER_REJECTED, _decode_lead_page
        )


class GetLeadOperation:
    """Read one Close lead by ID."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Close client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return manifest identity, branches, and retry defaults."""
        return GET_LEAD_DEFINITION

    async def invoke(self, call: Call, input: GetLeadInput) -> QueryAttempt[Lead]:
        """Validate the lead ID, read once, then select a branch or safe retry."""
        operation = GET_LEAD_OPERATION_ID
        if (
            not isinstance(input, GetLeadInput)
            or not isinstance(input.lead_id, str)
            or not input.lead_id.startswith("lead_")
            or _SAFE_KEY.fullmatch(input.lead_id) is None
        ):
            return _failed(
                operation, DEFECT, Lead(), FailureKind.VALIDATION, "leadId must be a Close lead ID"
            )
        return await self.client.invoke_get(
            call, operation, "/lead/" + input.lead_id + "/", Lead(), FOUND, NOT_FOUND, _decode_complete_lead
        )


def _failed(
    operation: str, branch: str, value: Output, kind: FailureKind, message: str
) -> QueryAttempt[Output]:
    return QueryAttempt.branch_result(
        branch, value, Receipt(), Failure(kind, CONNECTOR_ID, operation, message)
    )


def _retry(operation: str, kind: FailureKind, message: str, after_seconds: float) -> QueryAttempt[Any]:
    return QueryAttempt.retry(Failure(kind, CONNECTOR_ID, operation, message), after_seconds)


def _decode_lead_page(document: Any) -> LeadPage:
    if not isinstance(document, dict):
        raise ValueError
    leads = document.get("data")
    has_more = document.get("has_more")
    if has_more is None:
        has_more = False
    if not isinstance(leads, list) or not isinstance(has_more, bool):
        raise ValueError
    return LeadPage(tuple(_decode_lead(lead) for lead in leads), has_more)


def _decode_complete_lead(document: Any) -> Lead:
    lead = _decode_lead(document)
    if not lead.id or not lead.name:
        raise ValueError
    return lead


def _decode_lead(document: Any) -> Lead:
    if not isinstance(document, dict):
        raise ValueError
    return Lead(_string(document, "id"), _string(document, "name"))


def _string(document: dict[str, Any], name: str) -> str:
    value = document.get(name)
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError
    return value


Config.__doc__ = "Close HTTP settings; zero or blank values use manifest defaults."
Credentials.__doc__ = "Close API key, resolved anew for every provider attempt."
