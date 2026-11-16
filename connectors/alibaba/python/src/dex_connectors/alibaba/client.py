# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Alibaba Cloud ECS DescribeRegions with OpenAPI signature V3."""

import hashlib
import hmac
import json
import secrets
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any
from urllib.parse import urlsplit

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
from ._generated.definitions import DESCRIBE_REGIONS_DEFINITION
from .models import DescribeRegionsInput, DescribeRegionsOutput, ECSRegion

_EMPTY_PAYLOAD_SHA256 = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
_SIGNED_HEADER_NAMES = "host;x-acs-action;x-acs-content-sha256;x-acs-date;x-acs-signature-nonce;x-acs-version"


def _sign_acs3_request(
    secret: str, method: str, host: str, action: str, canonical_query: str, request_date: str, nonce: str
) -> str:
    canonical_headers = (
        f"host:{host}\n"
        f"x-acs-action:{action}\n"
        f"x-acs-content-sha256:{_EMPTY_PAYLOAD_SHA256}\n"
        f"x-acs-date:{request_date}\n"
        f"x-acs-signature-nonce:{nonce}\n"
        "x-acs-version:2014-05-26\n"
    )
    canonical_request = (
        f"{method}\n/\n{canonical_query}\n{canonical_headers}\n"
        f"{_SIGNED_HEADER_NAMES}\n{_EMPTY_PAYLOAD_SHA256}"
    )
    digest = hashlib.sha256(canonical_request.encode()).hexdigest()
    string_to_sign = f"ACS3-HMAC-SHA256\n{digest}"
    return hmac.new(secret.encode(), string_to_sign.encode(), hashlib.sha256).hexdigest()


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DescribeRegionsOutput]:
    failure = Failure(kind, "alibaba", "describeRegions", message)
    return QueryAttempt.branch_result(branch, DescribeRegionsOutput(), Receipt(), failure)


def _retry(kind: FailureKind, message: str, retry_after: float = 0) -> QueryAttempt[DescribeRegionsOutput]:
    return QueryAttempt.retry(Failure(kind, "alibaba", "describeRegions", message), retry_after)


def _valid_credential(value: str) -> bool:
    return len(value) <= 512 and is_header_safe_credential(value)


class Client:
    """Own immutable ECS settings and resolve RAM credentials for each call."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        *,
        clock: Callable[[], datetime] = lambda: datetime.now(UTC),
        nonce_source: Callable[[int], bytes] = secrets.token_bytes,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Alibaba Cloud configuration and credential source are required")
        if config.timeout_seconds <= 0 or config.max_response_bytes <= 0:
            raise ValueError("Alibaba Cloud timeout and maxResponseBytes must be positive")
        self.base_url = validate_base_url(config.base_url)
        self.endpoint_host = urlsplit(self.base_url).netloc
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self._clock = clock
        self._nonce_source = nonce_source

    def describe_regions(self) -> "DescribeRegionsOperation":
        """Bind the read-only ECS regions Query."""
        return DescribeRegionsOperation(self)

    async def aclose(self) -> None:
        """Close the provider transport when this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class DescribeRegionsOperation:
    """List ECS regions with a fresh signed request per Execute attempt."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("Alibaba Cloud client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query identity and retry defaults."""
        return DESCRIBE_REGIONS_DEFINITION

    async def invoke(self, call: Call, input: DescribeRegionsInput) -> QueryAttempt[DescribeRegionsOutput]:
        """Sign one request and classify the ECS response without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key_id = credentials.access_key_id.reveal()
            access_key_secret = credentials.access_key_secret.reveal()
            if not _valid_credential(access_key_id) or not _valid_credential(access_key_secret):
                raise ValueError("invalid AccessKey")
        except (ValueError, LookupError, OSError, AttributeError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials are unavailable or invalid"
            )
        try:
            nonce_bytes = self.client._nonce_source(16)
            if len(nonce_bytes) != 16:
                raise ValueError("wrong nonce length")
            nonce = nonce_bytes.hex()
        except (OSError, ValueError):
            return _retry(FailureKind.AVAILABILITY, "request nonce unavailable")
        request_date = self.client._clock().astimezone(UTC).strftime("%Y-%m-%dT%H:%M:%SZ")
        try:
            signature = _sign_acs3_request(
                access_key_secret,
                "GET",
                self.client.endpoint_host,
                "DescribeRegions",
                "",
                request_date,
                nonce,
            )
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + "/",
                headers={
                    "x-acs-action": "DescribeRegions",
                    "x-acs-content-sha256": _EMPTY_PAYLOAD_SHA256,
                    "x-acs-date": request_date,
                    "x-acs-signature-nonce": nonce,
                    "x-acs-version": "2014-05-26",
                    "Authorization": (
                        f"ACS3-HMAC-SHA256 Credential={access_key_id},SignedHeaders={_SIGNED_HEADER_NAMES},"
                        f"Signature={signature}"
                    ),
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError):
            return _branch("defect", FailureKind.VALIDATION, "ECS request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "ECS request failed")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if response.status_code == 429:
                return _retry(FailureKind.RATE_LIMIT, "ECS rate limited the request", retry_after)
            if response.status_code == 408 or response.status_code >= 500:
                return _retry(FailureKind.AVAILABILITY, "ECS is temporarily unavailable", retry_after)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "ECS response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "ECS response was interrupted")
        finally:
            await response.aclose()
        return _decode_regions(body, response.status_code, retry_after)


def _decode_regions(body: bytes, status: int, retry_after: float) -> QueryAttempt[DescribeRegionsOutput]:
    try:
        document: Any = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS returned invalid JSON")
    if not isinstance(document, dict):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS returned invalid JSON")
    code = document.get("Code", "")
    if code is None:
        code = ""
    if not isinstance(code, str):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS returned invalid JSON")
    if code.startswith("Throttling"):
        return _retry(FailureKind.RATE_LIMIT, "ECS rate limited the request", retry_after)
    if code or status != 200:
        return _branch("providerRejected", FailureKind.PROVIDER_REJECTION, "ECS rejected the regions request")
    regions = document.get("Regions")
    region_list = regions.get("Region") if isinstance(regions, dict) else None
    if not isinstance(region_list, list):
        return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS omitted its regions list")
    decoded: list[ECSRegion] = []
    for region in region_list:
        if not isinstance(region, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS returned an incomplete region")
        region_id = region.get("RegionId")
        local_name = region.get("LocalName")
        endpoint = region.get("RegionEndpoint")
        status_value = region.get("Status")
        if (
            not isinstance(region_id, str)
            or not region_id
            or not isinstance(local_name, str)
            or not local_name
            or not isinstance(endpoint, str)
            or not endpoint
            or not isinstance(status_value, str)
            or status_value not in ("available", "soldOut")
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "ECS returned an incomplete region")
        decoded.append(ECSRegion(region_id, local_name, endpoint, status_value))
    return QueryAttempt.branch_result("fetched", DescribeRegionsOutput(tuple(decoded)), Receipt())
