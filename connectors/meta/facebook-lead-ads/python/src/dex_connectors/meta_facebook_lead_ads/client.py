# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Meta Graph API metadata reads with Go-compatible provider decisions."""

import json
import re
from dataclasses import dataclass
from datetime import UTC, datetime
from typing import Any, TypeVar
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
from ._generated.definitions import GET_LEAD_DEFINITION, LIST_LEAD_FORMS_DEFINITION
from ._graph_error import decode_graph_error
from .models import GetLeadInput, LeadFormMetadata, LeadFormsPage, LeadMetadata, ListLeadFormsInput

Output = TypeVar("Output")
_NUMERIC_ID = re.compile(r"[0-9]{1,40}\Z")


@dataclass(frozen=True, slots=True)
class _Outcome:
    """A classified provider branch or retry."""

    branch: str
    failure: Failure
    is_retry: bool = False
    retry_after_seconds: float = 0

    def with_empty(self, empty: Output) -> QueryAttempt[Output]:
        """Apply this decision to the operation's empty output."""
        if self.is_retry:
            return QueryAttempt.retry(self.failure, self.retry_after_seconds)
        return QueryAttempt.branch_result(self.branch, empty, Receipt(), self.failure)


def _decision(operation: str, branch: str, kind: FailureKind, message: str) -> _Outcome:
    return _Outcome(branch, Failure(kind, "facebook-lead-ads", operation, message))


def _invalid(operation: str, empty: Output) -> QueryAttempt[Output]:
    return _decision(
        operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
    ).with_empty(empty)


def _numeric_id(value: object) -> bool:
    return isinstance(value, str) and _NUMERIC_ID.fullmatch(value) is not None


class Client:
    """Resolve a Page token per call and read bounded Graph API responses."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Facebook Lead Ads configuration and credentials are required")
        if type(config.max_response_bytes) is not int or config.max_response_bytes < 1:
            raise ValueError("Meta response limit must be positive")
        if config.timeout_seconds <= 0:
            raise ValueError("Meta timeout must be positive")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def list_lead_forms(self) -> "ListLeadFormsOperation":
        """Bind the Page lead-form Query."""
        return ListLeadFormsOperation(self)

    def get_lead(self) -> "GetLeadOperation":
        """Bind the lead metadata Query."""
        return GetLeadOperation(self)

    async def aclose(self) -> None:
        """Close the owned transport."""
        if self._owns_client:
            await self.http_client.aclose()

    async def get(self, call: Call, operation: str, path: str) -> Any:
        """Fetch JSON or classify a provider failure without exposing its body."""
        try:
            credential = await self.credentials.resolve(call)
            access_token = credential.page_access_token.reveal()
            if not is_header_safe_credential(access_token):
                raise ValueError("invalid Page token")
        except (ValueError, LookupError, OSError, AttributeError):
            return _decision(
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
                    "Authorization": "Bearer " + access_token,
                    "User-Agent": "Dex Connector (https://superdurable.io)",
                },
            )
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Outcome(
                "",
                Failure(FailureKind.TRANSPORT, "facebook-lead-ads", operation, "Meta request failed"),
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
                        "facebook-lead-ads",
                        operation,
                        "Meta rate limited the request",
                    ),
                    True,
                    retry_after,
                )
            if status == 408 or status >= 500:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.AVAILABILITY,
                        "facebook-lead-ads",
                        operation,
                        "Meta is temporarily unavailable",
                    ),
                    True,
                    retry_after,
                )
            if status >= 400:
                try:
                    error_body = await read_bounded_body(response, self.max_response_bytes)
                except (BodyTooLargeError, ProviderTransportError):
                    error_body = b""
                if access_token.encode() in error_body:
                    return _decision(
                        operation,
                        "defect",
                        FailureKind.AUTHENTICATION,
                        "connection credentials are unavailable or invalid",
                    )
                graph_error = decode_graph_error(error_body)
                if graph_error is not None and (graph_error.is_throttled or graph_error.is_transient):
                    kind = FailureKind.RATE_LIMIT if graph_error.is_throttled else FailureKind.AVAILABILITY
                    message = (
                        "Meta rate limited the request"
                        if graph_error.is_throttled
                        else "Meta reported a transient error"
                    )
                    return _Outcome(
                        "", Failure(kind, "facebook-lead-ads", operation, message), True, retry_after
                    )
            if status == 404:
                return _decision(
                    operation, "notFound", FailureKind.NOT_FOUND, "Meta did not find the resource"
                )
            if status != 200:
                return _decision(
                    operation,
                    "providerRejected",
                    FailureKind.PROVIDER_REJECTION,
                    f"Meta rejected HTTP {status}",
                )
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _decision(
                    operation,
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "Meta response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _Outcome(
                    "",
                    Failure(
                        FailureKind.TRANSPORT, "facebook-lead-ads", operation, "Meta response was interrupted"
                    ),
                    True,
                )
            if access_token.encode() in body:
                return _decision(
                    operation,
                    "defect",
                    FailureKind.AUTHENTICATION,
                    "connection credentials are unavailable or invalid",
                )
            try:
                return json.loads(body)
            except (ValueError, UnicodeDecodeError):
                return _decision(
                    operation, "invalidResponse", FailureKind.PROTOCOL, "Meta returned invalid JSON"
                )
        finally:
            await response.aclose()


class ListLeadFormsOperation:
    """List one Page of form IDs, names, and statuses."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated form-list contract."""
        return LIST_LEAD_FORMS_DEFINITION

    async def invoke(self, call: Call, input: ListLeadFormsInput) -> QueryAttempt[LeadFormsPage]:
        """Read a Page of forms without questions or lead answers."""
        empty = LeadFormsPage()
        if (
            not isinstance(input, ListLeadFormsInput)
            or not _numeric_id(input.page_id)
            or not isinstance(input.after, str)
            or len(input.after) > 4096
            or any(char in input.after for char in "\r\n\x00")
        ):
            return _decision(
                "listLeadForms", "defect", FailureKind.VALIDATION, "Page ID or cursor is invalid"
            ).with_empty(empty)
        query = {"fields": "id,name,status", "limit": "100"}
        if input.after:
            query["after"] = input.after
        response = await self.client.get(
            call, "listLeadForms", f"/{input.page_id}/leadgen_forms?{urlencode(query)}"
        )
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or not isinstance(response.get("data"), list)
            or len(response["data"]) > 100
        ):
            return _invalid("listLeadForms", empty)
        paging = response.get("paging") or {}
        if not isinstance(paging, dict):
            return _invalid("listLeadForms", empty)
        cursors = paging.get("cursors") or {}
        if not isinstance(cursors, dict):
            return _invalid("listLeadForms", empty)
        after = cursors.get("after") or ""
        if not isinstance(after, str) or len(after) > 4096:
            return _invalid("listLeadForms", empty)
        forms = []
        for item in response["data"]:
            if (
                not isinstance(item, dict)
                or not _numeric_id(item.get("id"))
                or not isinstance(item.get("name"), str)
                or not item["name"]
                or not isinstance(item.get("status", ""), str)
            ):
                return _invalid("listLeadForms", empty)
            forms.append(LeadFormMetadata(item["id"], item["name"], item.get("status", "")))
        return QueryAttempt.branch_result("listed", LeadFormsPage(tuple(forms), after), Receipt())


class GetLeadOperation:
    """Read lead identity without answer fields."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated lead metadata contract."""
        return GET_LEAD_DEFINITION

    async def invoke(self, call: Call, input: GetLeadInput) -> QueryAttempt[LeadMetadata]:
        """Read one lead's ID, form, ad, and creation time."""
        empty = LeadMetadata()
        if not isinstance(input, GetLeadInput) or not _numeric_id(input.lead_id):
            return _decision(
                "getLead", "defect", FailureKind.VALIDATION, "lead ID must be numeric"
            ).with_empty(empty)
        query = urlencode({"fields": "id,created_time,form_id,ad_id"})
        response = await self.client.get(call, "getLead", f"/{input.lead_id}?{query}")
        if isinstance(response, _Outcome):
            return response.with_empty(empty)
        if (
            not isinstance(response, dict)
            or not _numeric_id(response.get("id"))
            or not _numeric_id(response.get("form_id"))
            or not isinstance(response.get("created_time"), str)
            or not response["created_time"]
            or not isinstance(response.get("ad_id", ""), str)
            or (response.get("ad_id", "") and not _numeric_id(response["ad_id"]))
        ):
            return _invalid("getLead", empty)
        return QueryAttempt.branch_result(
            "found",
            LeadMetadata(
                response["id"], response["form_id"], response.get("ad_id", ""), response["created_time"]
            ),
            Receipt(),
        )
