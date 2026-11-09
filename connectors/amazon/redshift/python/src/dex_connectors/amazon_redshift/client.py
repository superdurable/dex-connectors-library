# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed Amazon Redshift result-page reads with Go-compatible outcomes."""

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
from ._generated.definitions import GET_STATEMENT_RESULT_DEFINITION
from .models import GetStatementResultInput, StatementResultPage
from .sigv4 import sign_headers

_STATEMENT_ID = re.compile(r"^[a-z0-9]{8}(?:-[a-z0-9]{4}){3}-[a-z0-9]{12}(?::[0-9]{1,2})?$")
_REGION = re.compile(r"^[a-z]{2}(?:-gov)?-[a-z]+-[0-9]+$")


@dataclass(frozen=True, slots=True)
class _Exchange:
    """One bounded provider response or local failure."""

    status: int = 0
    body: bytes = b""
    retry_after: float = 0
    error: str = ""


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, "amazon-redshift", "getStatementResult", message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[StatementResultPage]:
    return QueryAttempt.branch_result(branch, StatementResultPage(), Receipt(), _failure(kind, message))


def _retry(kind: FailureKind, message: str, delay: float = 0) -> QueryAttempt[StatementResultPage]:
    return QueryAttempt.retry(_failure(kind, message), delay)


class Client:
    """Resolve AWS credentials and sign each GetStatementResult request."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        now: Callable[[], datetime] | None = None,
    ) -> None:
        if config is None or credentials is None:
            raise ValueError("Redshift configuration and credential source are required")
        if (
            not _REGION.fullmatch(config.region)
            or config.timeout_seconds <= 0
            or config.max_response_bytes < 1
        ):
            raise ValueError("Redshift Region or request limits are invalid")
        endpoint = config.endpoint or f"https://redshift-data.{config.region}.amazonaws.com"
        self.endpoint = validate_base_url(endpoint).rstrip("/")
        self.region = config.region
        self.max_response_bytes = config.max_response_bytes
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(config.timeout_seconds)
        self._owns_client = http_client is None
        self.now = now or (lambda: datetime.now(UTC))

    def get_statement_result(self) -> GetStatementResultOperation:
        """Bind the getStatementResult Query to this client."""
        return GetStatementResultOperation(self)

    async def aclose(self) -> None:
        """Close an owned transport after Worker shutdown."""
        if self._owns_client:
            await self.http_client.aclose()

    async def _get_statement_result(self, call: Call, body: bytes) -> _Exchange:
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
                "Content-Type": "application/x-amz-json-1.1",
                "X-Amz-Target": "RedshiftData.GetStatementResult",
            },
            body=body,
            access_key_id=access_key,
            secret_access_key=secret_key,
            session_token=session_token,
            region=self.region,
            service="redshift-data",
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


class GetStatementResultOperation:
    """Fetch one cached SQL result page by statement UUID."""

    def __init__(self, client: Client) -> None:
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return the generated Query contract."""
        return GET_STATEMENT_RESULT_DEFINITION

    async def invoke(self, call: Call, input: GetStatementResultInput) -> QueryAttempt[StatementResultPage]:
        """Classify signed provider outcomes and bounded JSON result pages."""
        if (
            not isinstance(input, GetStatementResultInput)
            or not isinstance(input.statement_id, str)
            or not _STATEMENT_ID.fullmatch(input.statement_id)
            or not isinstance(input.next_token, str)
            or len(input.next_token) > 4096
            or any(character in input.next_token for character in "\x00\r\n")
        ):
            return _branch("defect", FailureKind.VALIDATION, "Redshift statement ID or page token is invalid")
        payload: dict[str, str] = {"Id": input.statement_id}
        if input.next_token:
            payload["NextToken"] = input.next_token
        body = json.dumps(payload, separators=(",", ":"), ensure_ascii=False).encode()
        exchange = await self.client._get_statement_result(call, body)
        if exchange.error == "credential":
            return _branch(
                "defect", FailureKind.AUTHENTICATION, "Redshift credentials are unavailable or invalid"
            )
        if exchange.error == "request":
            return _retry(FailureKind.TRANSPORT, "Redshift request failed")
        if exchange.status == 429 or (exchange.status != 200 and _is_throttled(exchange.body)):
            return _retry(FailureKind.RATE_LIMIT, "Redshift rate limited the request", exchange.retry_after)
        if exchange.status == 408 or exchange.status >= 500:
            return _retry(
                FailureKind.AVAILABILITY, "Redshift is temporarily unavailable", exchange.retry_after
            )
        if exchange.status == 404 or (
            exchange.status == 400 and _has_error_code(exchange.body, "ResourceNotFoundException")
        ):
            return QueryAttempt.branch_result("missing", StatementResultPage(), Receipt())
        if exchange.status != 200:
            return _branch(
                "providerRejected",
                FailureKind.PROVIDER_REJECTION,
                f"Redshift rejected HTTP {exchange.status}",
            )
        if exchange.error == "too_large":
            return _branch(
                "invalidResponse",
                FailureKind.RESPONSE_TOO_LARGE,
                "Redshift response exceeded the configured limit",
            )
        if exchange.error:
            return _retry(FailureKind.TRANSPORT, "Redshift response was interrupted")
        try:
            document = json.loads(exchange.body)
        except (ValueError, UnicodeDecodeError):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Redshift returned invalid result JSON")
        if not isinstance(document, dict):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Redshift returned invalid result JSON")
        records = document.get("Records")
        next_token = document.get("NextToken", "")
        total_num_rows = document.get("TotalNumRows", 0)
        if (
            not isinstance(records, list)
            or not isinstance(next_token, str)
            or not isinstance(total_num_rows, int)
            or isinstance(total_num_rows, bool)
            or total_num_rows < 0
            or any(
                not isinstance(row, list)
                or any(not isinstance(field, dict) or len(field) != 1 for field in row)
                for row in records
            )
        ):
            return _branch("invalidResponse", FailureKind.PROTOCOL, "Redshift returned invalid result JSON")
        return QueryAttempt.branch_result(
            "fetched", StatementResultPage(tuple(records), next_token, total_num_rows), Receipt()
        )


def _has_error_code(body: bytes, expected: str) -> bool:
    try:
        document = json.loads(body)
    except (ValueError, UnicodeDecodeError):
        return False
    raw_code = document.get("__type", "") if isinstance(document, dict) else ""
    return isinstance(raw_code, str) and raw_code.rsplit("#", 1)[-1] == expected


def _is_throttled(body: bytes) -> bool:
    return any(
        _has_error_code(body, code)
        for code in (
            "ThrottlingException",
            "ActiveStatementsExceededException",
            "ActiveSessionsExceededException",
        )
    )
