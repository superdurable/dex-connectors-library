# Copyright (c) 2026 Super Durable
# SPDX-License-Identifier: MIT
"""Signed CTYun Knowledge Base Q&A listing and Go-compatible outcome classification."""

from __future__ import annotations

import base64
import hashlib
import hmac
import json
import uuid
from collections.abc import Callable
from datetime import UTC, datetime, timedelta, timezone
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
    new_provider_http_client,
    parse_retry_after,
    read_bounded_body,
    send_traced,
    validate_base_url,
)

from ._generated.config import Config as Config
from ._generated.config import Credentials as Credentials
from ._generated.definitions import CONNECTOR_ID as CONNECTOR_ID
from ._generated.definitions import DEFECT as DEFECT
from ._generated.definitions import INVALID_RESPONSE as INVALID_RESPONSE
from ._generated.definitions import LIST_KNOWLEDGE_BASES_DEFINITION as LIST_KNOWLEDGE_BASES_DEFINITION
from ._generated.definitions import LIST_KNOWLEDGE_BASES_OPERATION_ID as LIST_KNOWLEDGE_BASES_OPERATION_ID
from ._generated.definitions import LISTED as LISTED
from ._generated.definitions import PROVIDER_REJECTED as PROVIDER_REJECTED
from .models import KnowledgeBaseSummary, ListKnowledgeBasesInput, ListKnowledgeBasesOutput

_DEFAULT_BASE_URL = "https://kqa-global.ctapi.ctyun.cn"
_DEFAULT_TIMEOUT_SECONDS = 10
_DEFAULT_MAX_RESPONSE_BYTES = 2_097_152
_KNOWLEDGE_BASES_PATH = "/openapi/v1/infobases/list"
_BEIJING = timezone(timedelta(hours=8))
_INT64_MAX = 2**63 - 1


def _valid_credential(value: str) -> bool:
    return bool(value) and len(value.encode()) <= 2_048 and all(33 <= ord(c) <= 126 for c in value)


def _failure(kind: FailureKind, message: str) -> Failure:
    return Failure(kind, CONNECTOR_ID, LIST_KNOWLEDGE_BASES_OPERATION_ID, message)


def _branch(branch: str, kind: FailureKind, message: str) -> QueryAttempt[ListKnowledgeBasesOutput]:
    return QueryAttempt.branch_result(branch, ListKnowledgeBasesOutput(), Receipt(), _failure(kind, message))


def _hmac_sha256(key: bytes, message: bytes) -> bytes:
    return hmac.new(key, message, hashlib.sha256).digest()


def _authorization(access_key: str, security_key: str, request_id: str, date: str) -> str:
    body_hash = hashlib.sha256(b"").hexdigest()
    canonical = f"ctyun-eop-request-id:{request_id}\neop-date:{date}\n\n{body_hash}"
    time_key = _hmac_sha256(security_key.encode(), date.encode())
    access_key_hash = _hmac_sha256(time_key, access_key.encode())
    date_key = _hmac_sha256(access_key_hash, date[:8].encode())
    signature = base64.b64encode(_hmac_sha256(date_key, canonical.encode())).decode()
    return access_key + " headers=ctyun-eop-request-id;eop-date Signature=" + signature


def _wire_field(document: dict[str, Any], name: str) -> Any:
    if name in document:
        return document[name]
    return next((value for key, value in document.items() if key.casefold() == name.casefold()), None)


def _status_code(value: Any) -> str:
    if isinstance(value, str) and value:
        return value
    if type(value) is int and 0 < value <= _INT64_MAX:
        return str(value)
    raise ValueError("invalid provider status")


class Client:
    """Own the fixed production origin and resolve private keys for each signed listing."""

    def __init__(
        self,
        config: Config,
        credentials: CredentialProvider[Credentials],
        http_client: httpx.AsyncClient | None = None,
        clock: Callable[[], datetime] | None = None,
        request_id_generator: Callable[[], str] | None = None,
    ) -> None:
        """Validate the documented origin and inject optional deterministic test dependencies."""
        if config is None or credentials is None:
            raise ValueError("CTYun settings and credential source are required")
        if config.timeout_seconds < 0 or config.max_response_bytes < 0:
            raise ValueError("CTYun timeout and maxResponseBytes must be positive")
        base_url = validate_base_url(config.base_url or _DEFAULT_BASE_URL)
        parsed = urlsplit(base_url)
        if parsed.netloc != "kqa-global.ctapi.ctyun.cn" or parsed.path or parsed.scheme != "https":
            raise ValueError("CTYun base URL must be the documented HTTPS production origin")
        self.base_url = base_url
        self.max_response_bytes = config.max_response_bytes or _DEFAULT_MAX_RESPONSE_BYTES
        self.credentials = credentials
        self.http_client = http_client or new_provider_http_client(
            config.timeout_seconds or _DEFAULT_TIMEOUT_SECONDS
        )
        self.clock = clock or (lambda: datetime.now(UTC))
        self.request_id_generator = request_id_generator or (lambda: str(uuid.uuid4()))

    def list_knowledge_bases(self) -> ListKnowledgeBasesOperation:
        """Bind the read-only knowledge-base metadata Query."""
        return ListKnowledgeBasesOperation(self)

    async def aclose(self) -> None:
        """Close the owned provider HTTP client after Worker shutdown."""
        await self.http_client.aclose()


class ListKnowledgeBasesOperation:
    """List accessible knowledge-base IDs, names, and file counts."""

    def __init__(self, client: Client) -> None:
        """Bind one validated CTYun client."""
        if client is None:
            raise ValueError("CTYun client is required")
        self.client = client

    def definition(self) -> QueryDefinition:
        """Return generated Query identity and branches."""
        return LIST_KNOWLEDGE_BASES_DEFINITION

    async def invoke(
        self, call: Call, input: ListKnowledgeBasesInput
    ) -> QueryAttempt[ListKnowledgeBasesOutput]:
        """Sign one GET and classify provider outcomes without persisting private fields."""
        try:
            credentials = await self.client.credentials.resolve(call)
            access_key = credentials.access_key.reveal()
            security_key = credentials.security_key.reveal()
            tenant_id = credentials.tenant_id.reveal()
        except Exception:  # noqa: BLE001
            return _branch(
                DEFECT,
                FailureKind.AUTHENTICATION,
                "CTYun credentials or tenant ID are unavailable or invalid",
            )
        if not all(_valid_credential(value) for value in (access_key, security_key, tenant_id)):
            return _branch(
                DEFECT,
                FailureKind.AUTHENTICATION,
                "CTYun credentials or tenant ID are unavailable or invalid",
            )
        try:
            request_id = self.client.request_id_generator()
        except Exception:  # noqa: BLE001
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "CTYun request ID could not be generated")
            )
        try:
            date = self.client.clock().astimezone(_BEIJING).strftime("%Y%m%dT%H%M%SZ")
            authorization = _authorization(access_key, security_key, request_id, date)
            request = self.client.http_client.build_request(
                "GET",
                self.client.base_url + _KNOWLEDGE_BASES_PATH,
                headers={
                    "ctyun-eop-request-id": request_id,
                    "eop-date": date,
                    "tenantId": tenant_id,
                    "Eop-Authorization": authorization,
                    "Accept": "application/json",
                },
            )
        except (httpx.HTTPError, ValueError, TypeError, OverflowError):
            return _branch(DEFECT, FailureKind.VALIDATION, "CTYun request is invalid")
        try:
            response, _ = await send_traced(self.client.http_client, request)
        except ProviderTransportError:
            return QueryAttempt.retry(
                _failure(FailureKind.AVAILABILITY, "CTYun knowledge-base transport failed")
            )
        try:
            status = response.status_code
            retry_after = parse_retry_after(response.headers.get("Retry-After", ""), datetime.now(UTC))
            if status == 429:
                return QueryAttempt.retry(
                    _failure(FailureKind.RATE_LIMIT, "CTYun knowledge-base request rate limited"),
                    retry_after,
                )
            if status == 408 or status >= 500:
                return QueryAttempt.retry(
                    _failure(
                        FailureKind.AVAILABILITY, "CTYun knowledge-base service temporarily unavailable"
                    ),
                    retry_after,
                )
            try:
                contents = await read_bounded_body(response, self.client.max_response_bytes)
            except BodyTooLargeError:
                return _branch(
                    INVALID_RESPONSE,
                    FailureKind.RESPONSE_TOO_LARGE,
                    "CTYun knowledge-base response exceeded the configured limit",
                )
            except ProviderTransportError:
                return QueryAttempt.retry(
                    _failure(FailureKind.AVAILABILITY, "CTYun knowledge-base response was interrupted")
                )
            if status != 200:
                kind = FailureKind.AUTHENTICATION if status in (401, 403) else FailureKind.PROVIDER_REJECTION
                return _branch(
                    PROVIDER_REJECTED, kind, f"CTYun rejected knowledge-base request with HTTP {status}"
                )
            try:
                document = json.loads(contents)
                if not isinstance(document, dict):
                    raise ValueError("response is not an object")
            except (ValueError, UnicodeDecodeError):
                return _branch(
                    INVALID_RESPONSE, FailureKind.PROTOCOL, "CTYun returned invalid knowledge-base JSON"
                )
            try:
                provider_status = _status_code(_wire_field(document, "statusCode"))
            except ValueError:
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "CTYun returned invalid status code")
            if provider_status != "200":
                error_value = _wire_field(document, "error")
                provider_error = error_value if isinstance(error_value, str) else ""
                kind = FailureKind.PROVIDER_REJECTION
                if provider_status in {"40001", "40003", "40016"} or provider_error.startswith("CTAPI_100"):
                    kind = FailureKind.AUTHENTICATION
                if provider_status in {"40002", "40004"}:
                    return QueryAttempt.retry(
                        _failure(
                            FailureKind.AVAILABILITY,
                            "CTYun knowledge-base service reported a temporary error",
                        )
                    )
                return _branch(
                    PROVIDER_REJECTED,
                    kind,
                    f"CTYun rejected knowledge-base request with status {provider_status}",
                )
            items = _wire_field(document, "returnObj")
            if not isinstance(items, list):
                return _branch(INVALID_RESPONSE, FailureKind.PROTOCOL, "CTYun knowledge-base list is missing")
            summaries: list[KnowledgeBaseSummary] = []
            for item in items:
                if not isinstance(item, dict):
                    return _branch(
                        INVALID_RESPONSE,
                        FailureKind.PROTOCOL,
                        "CTYun returned invalid knowledge-base metadata",
                    )
                resource_id = _wire_field(item, "id")
                name = _wire_field(item, "name")
                count = _wire_field(item, "count")
                if count is None:
                    count = 0
                if (
                    type(resource_id) is not int
                    or not 0 < resource_id <= _INT64_MAX
                    or not isinstance(name, str)
                    or not name
                    or len(name.encode()) > 1_024
                    or type(count) is not int
                    or not 0 <= count <= _INT64_MAX
                ):
                    return _branch(
                        INVALID_RESPONSE,
                        FailureKind.PROTOCOL,
                        "CTYun returned invalid knowledge-base metadata",
                    )
                summaries.append(KnowledgeBaseSummary(resource_id, name, count))
            return QueryAttempt.branch_result(LISTED, ListKnowledgeBasesOutput(tuple(summaries)), Receipt())
        finally:
            await response.aclose()


Config.__doc__ = "CTYun fixed HTTPS origin and response bounds; blank or zero use manifest defaults."
Credentials.__doc__ = "Private access key, security key, and tenant ID, resolved on every list call."
