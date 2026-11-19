# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Sensors Data campaign Query and Go-compatible provider classification."""

import json
import re
from datetime import UTC, datetime

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
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID as CONNECTOR_ID,
)
from ._generated.definitions import (
    DEFECT as DEFECT,
)
from ._generated.definitions import (
    INVALID_RESPONSE as INVALID_RESPONSE,
)
from ._generated.definitions import (
    LIST_CAMPAIGNS_DEFINITION as LIST_CAMPAIGNS_DEFINITION,
)
from ._generated.definitions import (
    LIST_CAMPAIGNS_OPERATION_ID,
)
from ._generated.definitions import (
    LISTED as LISTED,
)
from ._generated.definitions import (
    PROVIDER_REJECTED as PROVIDER_REJECTED,
)
from .models import Campaign, ListCampaignsInput, ListCampaignsOutput

_CAMPAIGNS_PATH = "/api/v3/analytics/v1/channel/campaigns/list"
_PROJECT_NAME = re.compile(r"^[A-Za-z0-9_-]{1,128}$")


class Client:
    """Bounded Analytics client resolving the API key for each Query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Sensors Data configuration and credentials are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Sensors Data timeout and response limit must be positive")
        if _PROJECT_NAME.fullmatch(config.project_name) is None:
            raise ValueError("Sensors Data project name must be an ASCII identifier")
        self.base_url = validate_base_url(config.base_url).rstrip("/")
        if httpx.URL(self.base_url).path not in ("", "/"):
            raise ValueError("Sensors Data API URL must be a host root")
        self.project_name = config.project_name
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def list_campaigns(self) -> "ListCampaignsOperation":
        """Bind the channel campaign Query to this client."""
        return ListCampaignsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class ListCampaignsOperation:
    """Read one bounded channel campaign page."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Sensors Data client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_CAMPAIGNS_DEFINITION

    async def invoke(self, call: Call, input: ListCampaignsInput) -> QueryAttempt[ListCampaignsOutput]:
        """Make one provider request and classify its observed outcome."""
        try:
            credentials = await self.client.credentials.resolve(call)
            api_key = credentials.api_key.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Sensors Data API key unavailable")
        if not isinstance(api_key, str) or len(api_key) != 35 or any(not "!" <= c <= "~" for c in api_key):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "Sensors Data API key invalid")
        if not isinstance(input, ListCampaignsInput):
            return _branch(DEFECT, FailureKind.VALIDATION, "Sensors Data pagination invalid")
        page_size = input.page_size or 20
        if (
            type(input.page_num) is not int
            or type(page_size) is not int
            or input.page_num < 0
            or input.page_num > 100000
            or page_size < 1
            or page_size > 1000
        ):
            return _branch(DEFECT, FailureKind.VALIDATION, "Sensors Data pagination invalid")
        body = json.dumps(
            {"page_num": input.page_num, "page_size": page_size}, separators=(",", ":")
        ).encode()
        request = self.client.http_client.build_request(
            "POST",
            self.client.base_url + _CAMPAIGNS_PATH,
            content=body,
            headers={
                "Accept": "application/json",
                "Content-Type": "application/json",
                "api-key": api_key,
                "sensorsdata-project": self.client.project_name,
            },
        )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "Sensors Data request failed")
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "Sensors Data rate limited the request", delay)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "Sensors Data is unavailable", delay)
            try:
                raw_body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE, FailureKind.RESPONSE_TOO_LARGE, "Sensors Data response too large"
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "Sensors Data response interrupted")
            if response.status_code != 200:
                kind = {401: FailureKind.AUTHENTICATION, 403: FailureKind.AUTHORIZATION}.get(
                    response.status_code, FailureKind.PROVIDER_REJECTION
                )
                return _branch(PROVIDER_REJECTED, kind, "Sensors Data rejected HTTP request")
            try:
                document = json.loads(raw_body.decode("utf-8"), parse_constant=_invalid_constant)
                if not isinstance(document, dict) or not isinstance(document.get("code"), str):
                    raise ValueError("invalid campaign document")
                if not document["code"]:
                    raise ValueError("missing campaign status")
                if document["code"] != "SUCCESS":
                    return _branch(
                        PROVIDER_REJECTED,
                        FailureKind.PROVIDER_REJECTION,
                        "Sensors Data rejected campaign query",
                    )
                data = document.get("data")
                if not isinstance(data, dict):
                    raise ValueError("missing campaign data")
                total_pages, page_num, returned_size = (
                    data.get("total_page"),
                    data.get("page_num"),
                    data.get("page_size"),
                )
                if (
                    type(total_pages) is not int
                    or total_pages < 0
                    or type(page_num) is not int
                    or page_num < 0
                    or type(returned_size) is not int
                    or not 1 <= returned_size <= 1000
                ):
                    raise ValueError("invalid pagination")
                total_rows = _parse_total_rows(data.get("total_rows"))
                details = data.get("campaign_details") or []
                if not isinstance(details, list) or len(details) > page_size:
                    raise ValueError("invalid campaign count")
                campaigns: list[Campaign] = []
                for item in details:
                    if not isinstance(item, dict):
                        raise ValueError("invalid campaign")
                    name, count, latest = (
                        item.get("campaign_name", ""),
                        item.get("branch_num"),
                        item.get("latest_use_time", ""),
                    )
                    if (
                        not isinstance(name, str)
                        or not name
                        or len(name.encode()) > 1024
                        or type(count) is not int
                        or count < 0
                        or not isinstance(latest, str)
                        or len(latest.encode()) > 64
                    ):
                        raise ValueError("invalid campaign metadata")
                    campaigns.append(Campaign(name, count, latest))
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "Sensors Data returned invalid campaign data"
                )
            return QueryAttempt.branch_result(
                LISTED,
                ListCampaignsOutput(tuple(campaigns), total_rows, total_pages, page_num, returned_size),
                Receipt(),
            )
        finally:
            await response.aclose()


def _parse_total_rows(value: object) -> int:
    if type(value) is int:
        total = value
    elif isinstance(value, str) and re.fullmatch(r"-?[0-9]+", value):
        total = int(value)
    else:
        raise ValueError("invalid total rows")
    if not 0 <= total <= 2**63 - 1:
        raise ValueError("total rows outside int64")
    return total


def _invalid_constant(value: str) -> None:
    raise ValueError(f"invalid JSON constant {value}")


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListCampaignsOutput]:
    return QueryAttempt.branch_result(
        branch,
        ListCampaignsOutput(),
        Receipt(),
        Failure(kind, CONNECTOR_ID, LIST_CAMPAIGNS_OPERATION_ID, message),
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ListCampaignsOutput]:
    return QueryAttempt.retry(
        Failure(kind, CONNECTOR_ID, LIST_CAMPAIGNS_OPERATION_ID, message),
        delay,
    )


Config.__doc__ = "Sensors Data HTTPS origin, project, timeout in seconds, and byte bound."
Credentials.__doc__ = "Private 35-character Analytics API key resolved for each Query."
