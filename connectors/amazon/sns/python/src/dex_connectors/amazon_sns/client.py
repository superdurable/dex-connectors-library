# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Amazon SNS topic listing with Go-compatible outcomes."""

from __future__ import annotations

import re
from collections.abc import Callable
from dataclasses import dataclass
from datetime import UTC, datetime
from urllib.parse import urlencode
from xml.etree import ElementTree

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
from ._generated.definitions import LIST_TOPICS_DEFINITION
from .models import ListTopicsInput, TopicPage
from .sigv4 import sign_headers

_NEXT_TOKEN = re.compile(r"^[A-Za-z0-9+/=._-]*$")
_REGION = re.compile(r"^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "amazon-sns", "listTopics", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[TopicPage]:
    return QueryAttempt.branch_result(branch, TopicPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[TopicPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve AWS credentials and sign each ListTopics request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("SNS configuration and credential source are required")
        if (
            not _REGION.fullmatch(config.region)
            or config.timeout_seconds <= 0
            or config.max_response_bytes < 1
        ):
            raise ValueError("SNS Region or request limits are invalid")
        endpoint = config.endpoint or f"https://sns.{config.region}.amazonaws.com"
        self.endpoint = validate_base_url(endpoint).rstrip("/")
        self.region = config.region
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def list_topics(self) -> ListTopicsOperation:
        """Bind the listTopics Query to this client."""
        return ListTopicsOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _list_topics(self, call: Call, next_token: str) -> _Exchange:
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
        parameters = {"Action": "ListTopics", "Version": "2010-03-31"}
        if next_token:
            parameters["NextToken"] = next_token
        url = self.endpoint + "/?" + urlencode(parameters)
        headers = sign_headers(
            method="GET",
            url=url,
            headers={},
            body=b"",
            access_key_id=access_key,
            secret_access_key=secret_key,
            session_token=session_token,
            region=self.region,
            service="sns",
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


class ListTopicsOperation:
    """Read one page of regional SNS topics."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return LIST_TOPICS_DEFINITION

    async def invoke(self, call: Call, input: ListTopicsInput) -> QueryAttempt[TopicPage]:
        """Classify signed provider outcomes and bounded metadata."""
        if (
            not isinstance(input, ListTopicsInput)
            or len(input.next_token) > 4096
            or not _NEXT_TOKEN.fullmatch(input.next_token)
        ):
            return _branch("defect", FailureKind.VALIDATION, "SNS pagination token is invalid")
        exchange = await self.client._list_topics(call, input.next_token)
        if exchange.error == "credential":
            return _branch("defect", FailureKind.AUTHENTICATION, "SNS credentials are unavailable or invalid")
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "SNS request failed")
        if exchange.status == 429:
            return _retry(FailureKind.RATE_LIMIT, "SNS rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(FailureKind.AVAILABILITY, "SNS is temporarily unavailable", exchange.retry_after)
        if exchange.status != 200:
            return _branch(
                "providerRejected", FailureKind.PROVIDER_REJECTION, f"SNS rejected HTTP {exchange.status}"
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "SNS response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "SNS response was interrupted")
        try:
            root = ElementTree.fromstring(exchange.body)
        except ElementTree.ParseError:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "SNS returned invalid topic XML")
        namespace = root.tag.removesuffix("ListTopicsResponse")
        if namespace not in {
            "{https://sns.amazonaws.com/doc/2010-03-31/}",
            "{http://sns.amazonaws.com/doc/2010-03-31/}",
        } or root.tag != namespace + "ListTopicsResponse":
            return _branch("invalidResponse", FailureKind.PROTOCOL, "SNS returned invalid topic XML")
        result = root.find(namespace + "ListTopicsResult")
        if result is None:
            return _branch("invalidResponse", FailureKind.PROTOCOL, "SNS returned invalid topic XML")
        topics = result.findall(namespace + "Topics/" + namespace + "member")
        arns = tuple((topic.findtext(namespace + "TopicArn") or "") for topic in topics)
        next_token = result.findtext(namespace + "NextToken") or ""
        if (
            len(arns) > 100
            or any(not arn for arn in arns)
            or len(next_token) > 4096
            or not _NEXT_TOKEN.fullmatch(next_token)
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "SNS returned invalid topic XML")
        return QueryAttempt.branch_result("page", TopicPage(arns, next_token), Receipt())
