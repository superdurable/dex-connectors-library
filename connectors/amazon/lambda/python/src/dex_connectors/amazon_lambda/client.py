# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Amazon Lambda function reads with Go-compatible outcomes."""

from __future__ import annotations

import json
import re
from collections.abc import Callable
from dataclasses import dataclass
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
    is_header_safe_credential,
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import GET_FUNCTION_DEFINITION
from .models import FunctionDetails, GetFunctionInput
from .sigv4 import sign_headers

_FUNCTION_NAME = re.compile(r"^[A-Za-z0-9_-]{1,64}$")
_REGION = re.compile(r"^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "amazon-lambda", "getFunction", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[FunctionDetails]:
    return QueryAttempt.branch_result(branch, FunctionDetails(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[FunctionDetails]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve AWS credentials and sign each GetFunction request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Lambda configuration and credential source are required")
        if (
            not _REGION.fullmatch(config.region)
            or config.timeout_seconds <= 0
            or config.max_response_bytes < 1
        ):
            raise ValueError("Lambda Region or request limits are invalid")
        endpoint = config.endpoint or f"https://lambda.{config.region}.amazonaws.com"
        self.endpoint = validate_base_url(endpoint).rstrip("/")
        self.region = config.region
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def get_function(self) -> GetFunctionOperation:
        """Bind the getFunction Query to this client."""
        return GetFunctionOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get_function(self, call: Call, name: str) -> _Exchange:
        try:
            credentials = await self.credentials.resolve(call)
            access_key = credentials.access_key_id.reveal()
            secret_key = credentials.secret_access_key.reveal()
            session_token = credentials.session_token.reveal()
        except (ValueError, LookupError, OSError, AttributeError):
            return _Exchange(error="credential")
        if not all((is_header_safe_credential(access_key), is_header_safe_credential(secret_key))) or (
            session_token and not is_header_safe_credential(session_token)
        ):
            return _Exchange(error="credential")
        url = self.endpoint + "/2015-03-31/functions/" + name
        headers = sign_headers(
            method="GET",
            url=url,
            headers={},
            body=b"",
            access_key_id=access_key,
            secret_access_key=secret_key,
            session_token=session_token,
            region=self.region,
            service="lambda",
            signing_time=self.now(),
        )
        try:
            request = self.http_client.build_request("GET", url, headers=headers)
            response, _ = await send_traced(self.http_client, request)
        except (ProviderTransportError, httpx.HTTPError, ValueError):
            return _Exchange(error="request")
        try:
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), self.now())
            try:
                body = await read_bounded_body(response, self.max_response_bytes)
            except BodyTooLargeError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="too_large")
            except ProviderTransportError:
                return _Exchange(status=response.status_code, retry_after=retry_after, error="interrupted")
            if any(secret.encode() in body for secret in (access_key, secret_key, session_token) if secret):
                return _Exchange(status=response.status_code, retry_after=retry_after, error="credential")
            return _Exchange(status=response.status_code, body=body, retry_after=retry_after)
        finally:
            await response.aclose()


class GetFunctionOperation:
    """Read one Lambda function's name and ARN."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_FUNCTION_DEFINITION

    async def invoke(self, call: Call, input: GetFunctionInput) -> QueryAttempt[FunctionDetails]:
        """Classify signed provider outcomes and bounded metadata."""
        if not isinstance(input, GetFunctionInput) or not _FUNCTION_NAME.fullmatch(input.function_name):
            return _branch("defect", FailureKind.VALIDATION, "Lambda function name is invalid")
        exchange = await self.client._get_function(call, input.function_name)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Lambda credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Lambda request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "Lambda rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "Lambda is temporarily unavailable", exchange.retry_after)
        if exchange.status == 404:
            return QueryAttempt.branch_result("missing", FunctionDetails(), Receipt())
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"Lambda rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Lambda response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Lambda response was interrupted")
        try:
            document = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Lambda returned invalid function JSON")
        if not isinstance(document, dict) or not isinstance(document.get("Configuration"), dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Lambda returned invalid function JSON")
        configuration = document["Configuration"]
        name = configuration.get("FunctionName")
        arn = configuration.get("FunctionArn")
        if name != input.function_name or not isinstance(arn, str) or not arn:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Lambda returned invalid function JSON")
        return QueryAttempt.branch_result("found", FunctionDetails(name, arn), Receipt())
