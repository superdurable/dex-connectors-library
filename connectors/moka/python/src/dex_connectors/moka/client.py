# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Moka provider transport and Go-compatible Query decisions."""

import base64
import json
import re
from datetime import UTC, datetime
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_JOB_FUNCTIONS_DEFINITION
from .models import JobFunction, ListJobFunctionsInput, ListJobFunctionsOutput


def _opaque(value: str, limit: int) -> bool:
    return (
        isinstance(value, str) and 0 < len(value) <= limit and all(0x20 < ord(char) < 0x7F for char in value)
    )


class Client:
    """Resolve credentials for each listJobFunctions request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or not 0 < config.max_response_bytes <= 16 << 20
        ):
            raise ValueError("Moka configuration and credentials are required")
        pass
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None

    def list_job_functions(self) -> "ListJobFunctionsOperation":
        """Bind the listJobFunctions Query."""
        return ListJobFunctionsOperation(self)

    async def aclose(self) -> None:
        """Close the owned HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListJobFunctionsOperation:
    """Query Moka and classify the provider outcome."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_JOB_FUNCTIONS_DEFINITION

    def _branch(self, branch: str, kind: FailureKind) -> QueryAttempt[ListJobFunctionsOutput]:
        return QueryAttempt.branch_result(
            branch,
            ListJobFunctionsOutput(),
            Receipt(),
            Failure(kind, "moka", "listJobFunctions", "Moka request failed"),
        )

    def _retry(self, kind: FailureKind, delay: float = 0) -> QueryAttempt[ListJobFunctionsOutput]:
        return QueryAttempt.retry(Failure(kind, "moka", "listJobFunctions", "Moka request failed"), delay)

    def _request(
        self, input: ListJobFunctionsInput, credentials: Credentials
    ) -> httpx.Request | QueryAttempt[ListJobFunctionsOutput]:
        if not re.fullmatch(
            r"[A-Za-z0-9_-]{1,128}", self.client.config.org_id
        ) or self.client.config.mode not in ("social", "campus"):
            return self._branch("defect", FailureKind.VALIDATION)
        token = credentials.api_key.reveal()
        if not _opaque(token, 1024):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        url = "https://api.mokahr.com/api-platform/v1/jobs-groupedby-zhineng/" + quote(
            self.client.config.org_id, safe=""
        )
        return self.client.http_client.build_request(
            "GET",
            url,
            params={"mode": self.client.config.mode},
            headers={
                "Accept": "application/json",
                "Authorization": "Basic " + base64.b64encode((token + ":").encode()).decode(),
            },
        )

    def _decode(
        self, wire: object, input: ListJobFunctionsInput, credentials: Credentials
    ) -> ListJobFunctionsOutput | QueryAttempt[ListJobFunctionsOutput] | None:
        if not isinstance(wire, dict):
            return None
        jobs = wire.get("jobs")
        if not isinstance(jobs, list):
            return None
        values = []
        for item in jobs:
            if (
                not isinstance(item, dict)
                or type(item.get("id")) is not int
                or item["id"] <= 0
                or not isinstance(item.get("label"), str)
                or not item["label"]
                or type(item.get("jobCount")) is not int
                or item["jobCount"] < 0
                or type(item.get("parentId", 0)) is not int
                or item.get("parentId", 0) < 0
            ):
                return None
            values.append(
                JobFunction(
                    id=item["id"],
                    label=item["label"],
                    parent_id=item.get("parentId", 0),
                    job_count=item["jobCount"],
                )
            )
        return ListJobFunctionsOutput(functions=tuple(values))

    async def invoke(self, call: Call, input: ListJobFunctionsInput) -> QueryAttempt[ListJobFunctionsOutput]:
        """Read one bounded provider response without persisting secrets."""
        if not isinstance(input, ListJobFunctionsInput):
            return self._branch("defect", FailureKind.VALIDATION)
        try:
            credentials = await self.client.credentials.resolve(call)
        except (ValueError, LookupError, OSError, AttributeError):
            return self._branch("defect", FailureKind.AUTHENTICATION)
        request = self._request(input, credentials)
        if isinstance(request, QueryAttempt):
            return request
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return self._retry(FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return self._retry(FailureKind.RATE_LIMIT, delay)
            if status == 408 or status >= 500:
                return self._retry(FailureKind.AVAILABILITY, delay)
            if status != 200:
                kind = (
                    FailureKind.AUTHENTICATION
                    if status == 401
                    else FailureKind.AUTHORIZATION
                    if status == 403
                    else FailureKind.PROVIDER_REJECTION
                )
                return self._branch("providerRejected", kind)
            try:
                body = await read_bounded_body(response, self.client.config.max_response_bytes)
            except BodyTooLargeError:
                return self._branch("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
            except ProviderTransportError:
                return self._retry(FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        try:
            wire = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        result = self._decode(wire, input, credentials)
        if isinstance(result, QueryAttempt):
            return result
        if result is None:
            return self._branch("invalidResponse", FailureKind.PROTOCOL)
        return QueryAttempt.branch_result("found", result, Receipt())
