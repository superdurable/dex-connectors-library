# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""West.cn reseller domain query."""

import hashlib
import json
from datetime import UTC, datetime
from urllib.parse import urlparse

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
from dex_connectors.sdk.providerhttp import new_provider_http_client, parse_retry_after

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import LIST_DOMAINS_DEFINITION
from .models import Domain, ListDomainsInput, ListDomainsOutput


def _valid_credential(value: str) -> bool:
    return (
        isinstance(value, str)
        and 0 < len(value) <= 512
        and value.strip() == value
        and all(0x20 < ord(c) < 0x7F for c in value)
    )


class Client:
    """Resolve rotated reseller credentials for each domain query."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        now: datetime | None = None,
    ) -> None:
        if (
            config is None
            or credentials is None
            or config.timeout_seconds <= 0
            or config.max_response_bytes <= 0
        ):
            raise ValueError("West configuration and credentials are required")
        parsed = urlparse(config.base_url)
        if (
            parsed.scheme not in {"https", "http"}
            or not parsed.netloc
            or parsed.path
            or parsed.query
            or parsed.fragment
        ):
            raise ValueError("West base URL must be an origin")
        self.config = config
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_http_client = http_client is None
        self.now = now

    def list_domains(self) -> "ListDomainsOperation":
        """Bind the generated listDomains Query."""
        return ListDomainsOperation(self)

    async def aclose(self) -> None:
        """Close an internally created HTTP transport."""
        if self._owns_http_client:
            await self.http_client.aclose()


class ListDomainsOperation:
    """Read one page of domains without exposing credentials to the Flow."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query definition."""
        return LIST_DOMAINS_DEFINITION

    def _failure(self, branch: str, kind: FailureKind) -> QueryAttempt[ListDomainsOutput]:
        failure = Failure(kind, "west", "listDomains", "West domain query failed")
        if branch == "retry":
            return QueryAttempt.retry(failure)
        return QueryAttempt.branch_result(branch, ListDomainsOutput(), Receipt(), failure)

    async def invoke(self, call: Call, input: ListDomainsInput) -> QueryAttempt[ListDomainsOutput]:
        """Fetch and validate a bounded GBK encoded domain page."""
        try:
            credentials = await self.client.credentials.resolve(call)
        except (LookupError, ValueError, OSError, AttributeError):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        if not _valid_credential(credentials.username) or not _valid_credential(
            credentials.api_password.reveal()
        ):
            return self._failure("defect", FailureKind.AUTHENTICATION)
        page = input.page or 1
        limit = input.limit or 20
        if not 1 <= page <= 1000000 or not 1 <= limit <= 100:
            return self._failure("defect", FailureKind.VALIDATION)
        timestamp = str(int((self.client.now or datetime.now(UTC)).timestamp() * 1000))
        token = hashlib.md5(
            (credentials.username + credentials.api_password.reveal() + timestamp).encode()
        ).hexdigest()
        params = {
            "act": "getdomains",
            "username": credentials.username,
            "time": timestamp,
            "token": token,
            "page": str(page),
            "limit": str(limit),
        }
        try:
            response = await self.client.http_client.get(
                self.client.config.base_url.rstrip("/") + "/api/v2/domain/",
                params=params,
                headers={"Accept": "application/json"},
            )
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        try:
            status = response.status_code
            if status == 429:
                return QueryAttempt.retry(
                    Failure(FailureKind.RATE_LIMIT, "west", "listDomains", "West rate limited the query"),
                    parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC)),
                )
            if status == 408 or status >= 500:
                return self._failure("retry", FailureKind.AVAILABILITY)
            body = await response.aread()
            if len(body) > self.client.config.max_response_bytes:
                return self._failure("invalidResponse", FailureKind.RESPONSE_TOO_LARGE)
        except httpx.HTTPError:
            return self._failure("retry", FailureKind.AVAILABILITY)
        finally:
            await response.aclose()
        if status != 200:
            kind = (
                FailureKind.AUTHENTICATION
                if status == 401
                else FailureKind.AUTHORIZATION
                if status == 403
                else FailureKind.PROVIDER_REJECTION
            )
            return self._failure("providerRejected", kind)
        try:
            wire = json.loads(body.decode("gbk"))
        except (UnicodeError, ValueError):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if not isinstance(wire, dict) or type(wire.get("result")) is not int:
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        if wire["result"] != 200:
            return self._failure("providerRejected", FailureKind.PROVIDER_REJECTION)
        data = wire.get("data")
        if (
            not isinstance(data, dict)
            or not isinstance(data.get("items"), list)
            or len(data["items"]) > limit
            or any(type(data.get(k)) is not int or data[k] < 0 for k in ("pageno", "totalpages", "total"))
        ):
            return self._failure("invalidResponse", FailureKind.PROTOCOL)
        domains: list[Domain] = []
        for item in data["items"]:
            if (
                not isinstance(item, dict)
                or not isinstance(item.get("domain"), str)
                or not isinstance(item.get("expdate"), str)
                or not item["domain"]
                or len(item["domain"]) > 253
                or item["domain"].strip() != item["domain"]
                or not 0 < len(item["expdate"]) <= 64
            ):
                return self._failure("invalidResponse", FailureKind.PROTOCOL)
            domains.append(Domain(item["domain"], item["expdate"]))
        return QueryAttempt.branch_result(
            "listed",
            ListDomainsOutput(tuple(domains), data["pageno"], data["totalpages"], data["total"]),
            Receipt(),
            None,
        )
