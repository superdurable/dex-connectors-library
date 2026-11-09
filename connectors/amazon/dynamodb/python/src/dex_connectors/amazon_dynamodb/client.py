# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Amazon DynamoDB item reads with Go-compatible outcomes."""

from __future__ import annotations

import base64
import binascii
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
from ._generated.definitions import GET_ITEM_DEFINITION
from .models import GetItemInput, ItemResult
from .sigv4 import sign_headers

_TABLE_NAME = re.compile(r"^[A-Za-z0-9_.-]{3,255}$")
_NUMBER = re.compile(r"^-?(?:0|[1-9][0-9]*)(?:\.[0-9]+)?(?:[Ee][+-]?[0-9]+)?$")
_REGION = re.compile(r"^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "amazon-dynamodb", "getItem", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ItemResult]:
    return QueryAttempt.branch_result(branch, ItemResult(item={}), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[ItemResult]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve AWS credentials and sign each GetItem request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("DynamoDB configuration and credential source are required")
        if (
            not _REGION.fullmatch(config.region)
            or config.timeout_seconds <= 0
            or config.max_response_bytes < 1
        ):
            raise ValueError("DynamoDB Region or request limits are invalid")
        endpoint = config.endpoint or f"https://dynamodb.{config.region}.amazonaws.com"
        self.endpoint = validate_base_url(endpoint).rstrip("/")
        self.region = config.region
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def get_item(self) -> GetItemOperation:
        """Bind the getItem Query to this client."""
        return GetItemOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get_item(self, call: Call, body: bytes) -> _Exchange:
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
        url = self.endpoint + "/"
        headers = sign_headers(
            method="POST",
            url=url,
            headers={
                "Content-Type": "application/x-amz-json-1.0",
                "X-Amz-Target": "DynamoDB_20120810.GetItem",
            },
            body=body,
            access_key_id=access_key,
            secret_access_key=secret_key,
            session_token=session_token,
            region=self.region,
            service="dynamodb",
            signing_time=self.now(),
        )
        try:
            request = self.http_client.build_request("POST", url, headers=headers, content=body)
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


class GetItemOperation:
    """Read one item by its complete primary key."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_ITEM_DEFINITION

    async def invoke(self, call: Call, input: GetItemInput) -> QueryAttempt[ItemResult]:
        """Classify signed provider outcomes and bounded metadata."""
        if (
            not isinstance(input, GetItemInput)
            or not isinstance(input.table_name, str)
            or not _TABLE_NAME.fullmatch(input.table_name)
            or not _valid_key(input.key)
        ):
            return _branch("defect", FailureKind.VALIDATION, "DynamoDB table or key is invalid")
        assert input.key is not None
        body = json.dumps(
            {
                "TableName": input.table_name,
                "Key": {name: input.key[name] for name in sorted(input.key)},
                "ConsistentRead": input.consistent_read,
            },
            separators=(",", ":"),
            ensure_ascii=False,
        ).encode()
        exchange = await self.client._get_item(call, body)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "DynamoDB credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "DynamoDB request failed")
        if exchange.status == 429 or (exchange.status != 200 and _is_throttled(exchange.body)):
            return _retry(FailureKind.RATE_LIMIT, "DynamoDB rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "DynamoDB is temporarily unavailable", exchange.retry_after
            )
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"DynamoDB rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "DynamoDB response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "DynamoDB response was interrupted")
        try:
            document = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "DynamoDB returned invalid item JSON")
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "DynamoDB returned invalid item JSON")
        item = document.get("Item")
        if item is None or item == {}:
            return QueryAttempt.branch_result("missing", ItemResult(item={}), Receipt())
        if not isinstance(item, dict) or any(
            not isinstance(name, str) or not name or not isinstance(value, dict) or len(value) != 1
            for name, value in item.items()
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "DynamoDB returned invalid item JSON")
        return QueryAttempt.branch_result("found", ItemResult(True, item), Receipt())


def _valid_key(key: object) -> bool:
    if not isinstance(key, dict) or len(key) not in (1, 2):
        return False
    for name, attribute in key.items():
        if (
            not isinstance(name, str)
            or not name
            or len(name) > 255
            or any(char in name for char in "\x00\r\n")
        ):
            return False
        if not isinstance(attribute, dict) or len(attribute) != 1:
            return False
        kind, value = next(iter(attribute.items()))
        if not isinstance(value, str) or not value:
            return False
        if kind == "N" and not _NUMBER.fullmatch(value):
            return False
        if kind == "B":
            try:
                base64.b64decode(value, validate=True)
            except (ValueError, binascii.Error):
                return False
        elif kind not in ("S", "N"):
            return False
    return True


def _is_throttled(body: bytes) -> bool:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    raw_code = document.get("__type", "") if isinstance(document, dict) else ""
    code = raw_code.rsplit("#", 1)[-1] if isinstance(raw_code, str) else ""
    return code in {
        "ProvisionedThroughputExceededException",
        "ThrottlingException",
        "RequestLimitExceeded",
        "TransactionConflictException",
    }
