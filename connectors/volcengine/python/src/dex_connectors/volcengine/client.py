# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""volcengine provider transport and Go-compatible Query decisions."""

import json
from collections.abc import Callable
from datetime import UTC, datetime
from typing import Any

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
from ._generated.definitions import DESCRIBE_REGIONS_DEFINITION
from .models import DescribeRegionsInput, DescribeRegionsOutput


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[DescribeRegionsOutput]:
    return QueryAttempt.branch_result(
        branch, DescribeRegionsOutput(), Receipt(), Failure(kind, "volcengine", "describeRegions", message)
    )


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[DescribeRegionsOutput]:
    return QueryAttempt.retry(Failure(kind, "volcengine", "describeRegions", message), delay)


class Client:
    """Resolve credentials per describeRegions request and own a redirect-safe HTTP client."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("volcengine configuration and credentials are required")
        if config.timeout_seconds <= 0 or not 0 < config.max_response_bytes <= 16 << 20:
            raise ValueError("volcengine timeout or response limit is invalid")
        self.config = config
        self.now = now or (lambda: datetime.now(UTC))
        self.credentials = credentials
        self.max_response_bytes = config.max_response_bytes
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None

    def describe_regions(self) -> "DescribeRegionsOperation":
        """Bind the describeRegions Query to this client."""
        return DescribeRegionsOperation(self)

    async def aclose(self) -> None:
        """Close the HTTP client if this client created it."""
        if self._owns_client:
            await self.http_client.aclose()


class DescribeRegionsOperation:
    """Perform the describeRegions read and return bounded provider data."""

    def __init__(self, client: Client) -> None:
        if client is None:
            raise ValueError("volcengine client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Go-derived Query identity."""
        return DESCRIBE_REGIONS_DEFINITION

    async def invoke(self, call: Call, input: DescribeRegionsInput) -> QueryAttempt[DescribeRegionsOutput]:
        """Classify transport and provider outcomes without exposing secrets."""
        try:
            credentials = await self.client.credentials.resolve(call)
            request = self._build_request(input, credentials)
        except (ValueError, LookupError, OSError, httpx.HTTPError):
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "connection credentials or request are invalid"
            )
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return _retry(FailureKind.AVAILABILITY, "volcengine request failed")
        return await self._process_response(response, input, credentials)

    async def _process_response(
        self, response: httpx.Response, input: DescribeRegionsInput, credentials: Credentials
    ) -> QueryAttempt[DescribeRegionsOutput]:
        try:
            delay = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            status = response.status_code
            if status == 429:
                return _retry(FailureKind.RATE_LIMIT, "volcengine rate limited the request", delay)
            if status == 408 or status >= 500:
                return _retry(FailureKind.AVAILABILITY, "volcengine is temporarily unavailable", delay)
            try:
                body = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    "invalidResponse",
                    FailureKind.RESPONSE_TOO_LARGE,
                    "volcengine response exceeded the configured limit",
                )
            except ProviderTransportError:
                return _retry(FailureKind.AVAILABILITY, "volcengine response was interrupted")
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
            return _branch("providerRejected", kind, f"volcengine rejected request with HTTP {status}")
        try:
            document: Any = json.loads(body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "volcengine returned invalid JSON")
        return self._decode(input, document)

    def _build_request(self, input: DescribeRegionsInput, credentials: Credentials) -> httpx.Request:
        import hashlib
        import hmac
        from urllib.parse import quote, urlsplit

        max_results = input.max_results or 10
        if (
            max_results < 1
            or max_results > 20
            or len(input.next_token) > 2048
            or input.next_token.strip() != input.next_token
        ):
            raise ValueError("invalid page")
        access_key = credentials.access_key_id.reveal()
        secret = credentials.secret_access_key.reveal()
        if not access_key or not secret:
            raise ValueError("missing credentials")
        base_url = validate_base_url(self.client.config.base_url)
        host = urlsplit(base_url).netloc
        values = {"Action": "DescribeRegions", "Version": "2020-04-01", "MaxResults": str(max_results)}
        if input.next_token:
            values["NextToken"] = input.next_token
        query = "&".join(f"{quote(key, safe='')}={quote(values[key], safe='')}" for key in sorted(values))
        request_date = self.client.now().astimezone(UTC).strftime("%Y%m%dT%H%M%SZ")
        canonical_headers = f"host:{host}\nx-date:{request_date}\n"
        payload_hash = hashlib.sha256(b"").hexdigest()
        canonical = "\n".join(("GET", "/", query, canonical_headers, "host;x-date", payload_hash))
        scope = f"{request_date[:8]}/{self.client.config.region}/{self.client.config.service}/request"
        to_sign = "\n".join(
            ("HMAC-SHA256", request_date, scope, hashlib.sha256(canonical.encode()).hexdigest())
        )
        key = secret.encode()
        for part in (request_date[:8], self.client.config.region, self.client.config.service, "request"):
            key = hmac.new(key, part.encode(), hashlib.sha256).digest()
        signature = hmac.new(key, to_sign.encode(), hashlib.sha256).hexdigest()
        authorization = (
            f"HMAC-SHA256 Credential={access_key}/{scope}, SignedHeaders=host;x-date, Signature={signature}"
        )
        return self.client.http_client.build_request(
            "GET",
            base_url + "/?" + query,
            headers={
                "X-Date": request_date,
                "Region": self.client.config.region,
                "Service": self.client.config.service,
                "Authorization": authorization,
                "Accept": "application/json",
            },
        )

    def _decode(self, input: DescribeRegionsInput, document: Any) -> QueryAttempt[DescribeRegionsOutput]:
        if not isinstance(document, dict) or not isinstance(document.get("ResponseMetadata"), dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid ECS envelope")
        metadata = document["ResponseMetadata"]
        error = metadata.get("Error")
        if isinstance(error, dict):
            code = error.get("Code")
            if code == "FlowLimitExceeded":
                return _retry(FailureKind.RATE_LIMIT, "ECS rate limited the request")
            if code == "InternalError":
                return _retry(FailureKind.AVAILABILITY, "ECS unavailable")
            kind = (
                FailureKind.AUTHORIZATION
                if code == "SignatureDoesNotMatch"
                else FailureKind.PROVIDER_REJECTION
            )
            return _branch("providerRejected", kind, "ECS rejected request")
        result = document.get("Result")
        if not isinstance(result, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid region page")
        regions = result.get("Regions")
        if (
            not isinstance(regions, list)
            or len(regions) > 20
            or not isinstance(result.get("NextToken", ""), str)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid region page")
        ids = []
        for region in regions:
            if (
                not isinstance(region, dict)
                or not isinstance(region.get("RegionId"), str)
                or not region["RegionId"]
            ):
                return _branch("invalidResponse", FailureKind.PROTOCOL, "invalid region ID")
            ids.append(region["RegionId"])
        return QueryAttempt.branch_result(
            "listed",
            DescribeRegionsOutput(region_ids=tuple(ids), next_token=result.get("NextToken", "")),
            Receipt(),
        )
