# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""BaishanCloud domain detail Query and Go-compatible outcome classification."""

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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import (
    CONNECTOR_ID,
    DEFECT,
    DESCRIBE_DOMAIN_DEFINITION,
    DESCRIBE_DOMAIN_OPERATION_ID,
    FOUND,
    INVALID_RESPONSE,
    NOT_FOUND,
    PROVIDER_REJECTED,
)
from .models import DescribeDomainInput, DomainDetail

_DOMAIN_PATTERN = re.compile(r"^[a-z0-9](?:[a-z0-9-]*[a-z0-9])?(?:\.[a-z0-9](?:[a-z0-9-]*[a-z0-9])?)+$")


class Client:
    """Bounded HTTP client that resolves the private CDN token for every request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("BaishanCloud configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("BaishanCloud timeout and response limit must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)

    def describe_domain(self) -> "DescribeDomainOperation":
        """Bind the domain-detail Query to this client."""
        return DescribeDomainOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport after Worker shutdown."""
        await self.http_client.aclose()


class DescribeDomainOperation:
    """Read one CDN domain without returning origin or cache settings."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("BaishanCloud client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return DESCRIBE_DOMAIN_DEFINITION

    async def invoke(self, call: Call, input: DescribeDomainInput) -> QueryAttempt[DomainDetail]:
        """Make one provider request and classify its observed outcome."""
        if not isinstance(input, DescribeDomainInput) or not isinstance(input.domain, str):
            return _branch(DEFECT, FailureKind.VALIDATION, "BaishanCloud domain name is invalid")
        domain = input.domain.strip().lower()
        if len(domain.encode("utf-8")) > 253 or not _DOMAIN_PATTERN.fullmatch(domain):
            return _branch(DEFECT, FailureKind.VALIDATION, "BaishanCloud domain name is invalid")
        try:
            credentials = await self.client.credentials.resolve(call)
            token = credentials.token.reveal()
        except Exception:
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "BaishanCloud token is unavailable or invalid")
        if not _valid_token(token):
            return _branch(DEFECT, FailureKind.AUTHENTICATION, "BaishanCloud token is unavailable or invalid")
        endpoint = self.client.base_url + "/v2/domain?" + urlencode({"domains": domain, "token": token})
        try:
            request = self.client.http_client.build_request(
                "GET", endpoint, headers={"Accept": "application/json"}
            )
        except (httpx.HTTPError, ValueError):
            return _branch(DEFECT, FailureKind.VALIDATION, "BaishanCloud domain request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except (ProviderTransportError, httpx.HTTPError):
            return _retry(FailureKind.AVAILABILITY, "BaishanCloud domain request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if retry_after == 0:
                retry_after = parse_retry_after(response.headers.get("X-Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "BaishanCloud rate limited the request", retry_after)
            if status == 408 or status >= 500:
                return _retry(
                    FailureKind.AVAILABILITY, "BaishanCloud is temporarily unavailable", retry_after
                )
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "BaishanCloud response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "BaishanCloud response was interrupted")
            if status != 200:
                kind = {
                    401: FailureKind.AUTHENTICATION,
                    403: FailureKind.AUTHORIZATION,
                    400: FailureKind.VALIDATION,
                    404: FailureKind.VALIDATION,
                }.get(status, FailureKind.PROVIDER_REJECTION)
                return _branch(PROVIDER_REJECTED, kind, f"BaishanCloud rejected the query with HTTP {status}")
            try:
                detail, found, code = _decode_detail(body, domain)
            except (ValueError, TypeError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "BaishanCloud returned an invalid domain response"
                )
            if code != 0:
                return _branch(
                    PROVIDER_REJECTED,
                    FailureKind.PROVIDER_REJECTION,
                    f"BaishanCloud rejected the query with code {code}",
                )
            if not found:
                return _branch(NOT_FOUND, FailureKind.NOT_FOUND, "BaishanCloud domain was not found")
            return QueryAttempt.branch_result(FOUND, detail, Receipt())
        finally:
            await response.aclose()


def _decode_detail(body: bytes, requested: str) -> tuple[DomainDetail, bool, int]:
    document = json.loads(
        body, parse_constant=lambda _: (_ for _ in ()).throw(ValueError("invalid constant"))
    )
    if not isinstance(document, dict) or type(document.get("code")) is not int:
        raise ValueError("invalid BaishanCloud response")
    code = document["code"]
    if code != 0:
        return DomainDetail(), False, code
    data = document.get("data")
    if not isinstance(data, list) or len(data) > 1:
        raise ValueError("invalid domain list")
    if not data:
        return DomainDetail(), False, 0
    item = data[0]
    if not isinstance(item, dict):
        raise ValueError("invalid domain detail")
    fields = {key: _go_string(item.get(key)) for key in ("id", "domain", "type", "status", "cname")}
    if not all(fields.values()) or fields["domain"].lower() != requested:
        raise ValueError("invalid domain detail")
    return DomainDetail(**fields), True, 0


def _go_string(value: object) -> str:
    if value is None:
        return ""
    if not isinstance(value, str):
        raise ValueError("invalid domain field")
    return value


def _valid_token(token: object) -> bool:
    return (
        isinstance(token, str)
        and 0 < len(token) <= 512
        and all("!" <= character <= "~" for character in token)
    )


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DomainDetail]:
    return QueryAttempt.branch_result(
        branch, DomainDetail(), Receipt(), Failure(kind, CONNECTOR_ID, DESCRIBE_DOMAIN_OPERATION_ID, message)
    )


def _retry(kind: FailureKind, message: str, after_seconds: float = 0) -> QueryAttempt[DomainDetail]:
    return QueryAttempt.retry(
        Failure(kind, CONNECTOR_ID, DESCRIBE_DOMAIN_OPERATION_ID, message), after_seconds
    )


Config.__doc__ = "BaishanCloud HTTPS origin, timeout in seconds, and response bound in bytes."
Credentials.__doc__ = "Private CDN API token resolved for each Query attempt."
